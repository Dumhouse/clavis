package cli

import (
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

func newCodegenCommand(opts *Options) *cobra.Command {
	var lang, outPath, packageName, typeName string
	cmd := &cobra.Command{
		Use:   "codegen",
		Short: "Generate typed config loaders from the project schema",
		Long: "Generate TypeScript (zod), Go (struct + loader), Python (pydantic), or\n" +
			"Rust (struct + loader) code from the project schema. Commit the generated\n" +
			"file next to your app.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" || resolved.Project == "" {
				return fmt.Errorf("no project context: run `clavis init` or pass --org and --project")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var schema schemaResponse
			if err := c.Get(cmd.Context(), schemaBase(resolved), &schema); err != nil {
				return err
			}
			if len(schema.Schema) == 0 {
				return fmt.Errorf("the project has no schema: run `clavis schema infer` or add keys in the UI")
			}
			var content string
			switch strings.ToLower(lang) {
			case "ts", "typescript":
				content, err = generateTypeScript(schema.Schema, typeName)
			case "go", "golang":
				content, err = generateGo(schema.Schema, packageName, typeName)
			case "py", "python":
				content, err = generatePython(schema.Schema, typeName)
			case "rs", "rust":
				content, err = generateRust(schema.Schema, typeName)
			default:
				return fmt.Errorf("unknown language %q: use ts, go, python, or rust", lang)
			}
			if err != nil {
				return err
			}
			if outPath == "" || outPath == "-" {
				_, err := fmt.Fprint(out.Out, content)
				return err
			}
			if err := os.WriteFile(outPath, []byte(content), 0o644); err != nil { //nolint:gosec // G306: generated source is meant to be committed and world-readable
				return fmt.Errorf("could not write %s: %w", outPath, err)
			}
			out.Success("Wrote %s.", outPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&lang, "lang", "ts", "ts, go, python, or rust")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output file (default stdout)")
	cmd.Flags().StringVar(&packageName, "package", "config", "Go package name")
	cmd.Flags().StringVar(&typeName, "name", "Config", "generated type/class name")
	return cmd
}

func schemaType(entry schemaJSON) string {
	if entry.Type == "" {
		return "string"
	}
	return entry.Type
}

func requiredAnywhere(entry schemaJSON) bool {
	for _, required := range entry.Required {
		if required {
			return true
		}
	}
	return false
}

func generateTypeScript(schema []schemaJSON, name string) (string, error) {
	if name == "" {
		name = "Config"
	}
	needBool, needJSON := false, false
	for _, entry := range schema {
		switch schemaType(entry) {
		case "bool":
			needBool = true
		case "json":
			needJSON = true
		}
	}
	var b strings.Builder
	b.WriteString("import { z } from 'zod'\n\n")
	if needBool {
		b.WriteString("const booleanString = z\n")
		b.WriteString("  .string()\n")
		b.WriteString("  .refine((value) => /^(true|false|1|0|yes|no)$/i.test(value), 'Must be a boolean')\n")
		b.WriteString("  .transform((value) => /^(true|1|yes)$/i.test(value))\n\n")
	}
	if needJSON {
		b.WriteString("const jsonString = z.string().transform((value, ctx) => {\n")
		b.WriteString("  try {\n")
		b.WriteString("    return JSON.parse(value) as unknown\n")
		b.WriteString("  } catch {\n")
		b.WriteString("    ctx.addIssue({ code: 'custom', message: 'Must be valid JSON' })\n")
		b.WriteString("    return z.NEVER\n")
		b.WriteString("  }\n")
		b.WriteString("})\n\n")
	}
	fmt.Fprintf(&b, "export const %sSchema = z.object({\n", lowerFirst(name))
	for _, entry := range schema {
		fmt.Fprintf(&b, "  %s: %s,\n", jsonString(entry.Key), tsField(entry))
	}
	b.WriteString("})\n\n")
	fmt.Fprintf(&b, "export type %s = z.infer<typeof %sSchema>\n\n", name, lowerFirst(name))
	fmt.Fprintf(&b, "export function load%s(env: Record<string, string | undefined> = process.env): %s {\n", name, name)
	fmt.Fprintf(&b, "  return %sSchema.parse({\n", lowerFirst(name))
	for _, entry := range schema {
		fmt.Fprintf(&b, "    %s: env[%s],\n", jsonString(entry.Key), jsonString(entry.Key))
	}
	b.WriteString("  })\n}\n")
	return b.String(), nil
}

func tsField(entry schemaJSON) string {
	var expression string
	switch schemaType(entry) {
	case "url":
		expression = "z.string().url()"
	case "int":
		expression = "z.coerce.number().int()"
	case "bool":
		expression = "booleanString"
	case "json":
		expression = "jsonString"
	case "enum":
		values := make([]string, 0, len(entry.Enum))
		for _, value := range entry.Enum {
			values = append(values, jsonString(value))
		}
		expression = "z.enum([" + strings.Join(values, ", ") + "])"
	default:
		expression = "z.string()"
	}
	if entry.Pattern != "" && (schemaType(entry) == "string" || schemaType(entry) == "url") {
		expression += ".regex(new RegExp(" + jsonString(entry.Pattern) + "))"
	}
	if !requiredAnywhere(entry) {
		expression += ".optional()"
	}
	return expression
}

func generateGo(schema []schemaJSON, packageName, typeName string) (string, error) {
	if packageName == "" {
		packageName = "config"
	}
	if typeName == "" {
		typeName = "Config"
	}
	names := goFieldNames(schema)
	needJSON, needURL, needRegexp, needStrconv, needFmt := false, false, false, false, false
	for _, entry := range schema {
		switch schemaType(entry) {
		case "json":
			needJSON = true
			needFmt = true
		case "url":
			needURL = true
			needFmt = true
		case "int":
			needStrconv = true
			needFmt = true
		case "bool", "enum":
			needFmt = true
		}
		if entry.Pattern != "" {
			needRegexp = true
			needFmt = true
		}
		if requiredAnywhere(entry) {
			needFmt = true
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated from the Clavis project schema. DO NOT EDIT.\n\npackage %s\n\nimport (\n", packageName)
	if needJSON {
		b.WriteString("\t\"encoding/json\"\n")
	}
	if needFmt {
		b.WriteString("\t\"fmt\"\n")
	}
	if needURL {
		b.WriteString("\t\"net/url\"\n")
	}
	b.WriteString("\t\"os\"\n")
	if needRegexp {
		b.WriteString("\t\"regexp\"\n")
	}
	if needStrconv {
		b.WriteString("\t\"strconv\"\n")
	}
	b.WriteString("\t\"strings\"\n)\n\n")
	fmt.Fprintf(&b, "// %s is typed configuration generated from the Clavis schema.\n", typeName)
	fmt.Fprintf(&b, "type %s struct {\n", typeName)
	for i, entry := range schema {
		if schemaType(entry) == "enum" {
			fmt.Fprintf(&b, "\t// %s values: %s\n", names[i], strings.Join(entry.Enum, ", "))
		}
		fmt.Fprintf(&b, "\t%s %s\n", names[i], goType(entry))
	}
	b.WriteString("}\n\n")
	for i, entry := range schema {
		if entry.Pattern == "" {
			continue
		}
		fmt.Fprintf(&b, "var pattern%s = regexp.MustCompile(%s)\n", names[i], strconv.Quote(entry.Pattern))
	}
	if needRegexp {
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "// Load reads %s from the process environment.\n", typeName)
	fmt.Fprintf(&b, "func Load() (%s, error) { return loadFrom(os.Environ(), \"\") }\n\n", typeName)
	fmt.Fprintf(&b, "// LoadFor reads %s and enforces required keys for envType.\n", typeName)
	fmt.Fprintf(&b, "func LoadFor(envType string) (%s, error) { return loadFrom(os.Environ(), envType) }\n\n", typeName)
	fmt.Fprintf(&b, "// LoadFrom reads KEY=VALUE pairs without enforcing required keys.\n")
	fmt.Fprintf(&b, "func LoadFrom(environ []string) (%s, error) { return loadFrom(environ, \"\") }\n\n", typeName)
	fmt.Fprintf(&b, "func loadFrom(environ []string, envType string) (%s, error) {\n", typeName)
	fmt.Fprintf(&b, "\tvar cfg %s\n", typeName)
	b.WriteString("\tvalues := make(map[string]string, len(environ))\n")
	b.WriteString("\tfor _, item := range environ {\n")
	b.WriteString("\t\tkey, value, ok := strings.Cut(item, \"=\")\n")
	b.WriteString("\t\tif ok {\n\t\t\tvalues[key] = value\n\t\t}\n")
	b.WriteString("\t}\n")
	for i, entry := range schema {
		field := names[i]
		fmt.Fprintf(&b, "\tif raw, ok := values[%s]; ok {\n", strconv.Quote(entry.Key))
		switch schemaType(entry) {
		case "url":
			b.WriteString("\t\tparsed, err := url.Parse(raw)\n")
			b.WriteString("\t\tif err != nil || parsed.Scheme == \"\" || parsed.Host == \"\" {\n")
			fmt.Fprintf(&b, "\t\t\treturn cfg, fmt.Errorf(%s)\n", strconv.Quote(entry.Key+": must be an absolute URL"))
			b.WriteString("\t\t}\n")
			fmt.Fprintf(&b, "\t\tcfg.%s = raw\n", field)
		case "int":
			b.WriteString("\t\tparsed, err := strconv.ParseInt(raw, 10, 64)\n")
			b.WriteString("\t\tif err != nil {\n")
			fmt.Fprintf(&b, "\t\t\treturn cfg, fmt.Errorf(%s)\n", strconv.Quote(entry.Key+": must be an integer"))
			b.WriteString("\t\t}\n")
			fmt.Fprintf(&b, "\t\tcfg.%s = int(parsed)\n", field)
		case "bool":
			b.WriteString("\t\tswitch strings.ToLower(raw) {\n")
			b.WriteString("\t\tcase \"true\", \"1\", \"yes\":\n")
			fmt.Fprintf(&b, "\t\t\tcfg.%s = true\n", field)
			b.WriteString("\t\tcase \"false\", \"0\", \"no\":\n")
			fmt.Fprintf(&b, "\t\t\tcfg.%s = false\n", field)
			b.WriteString("\t\tdefault:\n")
			fmt.Fprintf(&b, "\t\t\treturn cfg, fmt.Errorf(%s)\n", strconv.Quote(entry.Key+": must be a boolean"))
			b.WriteString("\t\t}\n")
		case "json":
			b.WriteString("\t\tif !json.Valid([]byte(raw)) {\n")
			fmt.Fprintf(&b, "\t\t\treturn cfg, fmt.Errorf(%s)\n", strconv.Quote(entry.Key+": must be valid JSON"))
			b.WriteString("\t\t}\n")
			fmt.Fprintf(&b, "\t\tcfg.%s = json.RawMessage(raw)\n", field)
		case "enum":
			values := make([]string, 0, len(entry.Enum))
			for _, value := range entry.Enum {
				values = append(values, strconv.Quote(value))
			}
			fmt.Fprintf(&b, "\t\tswitch raw {\n\t\tcase %s:\n", strings.Join(values, ", "))
			fmt.Fprintf(&b, "\t\t\tcfg.%s = raw\n", field)
			b.WriteString("\t\tdefault:\n")
			fmt.Fprintf(&b, "\t\t\treturn cfg, fmt.Errorf(%s)\n", strconv.Quote(entry.Key+": must be one of ["+strings.Join(entry.Enum, ", ")+"]"))
			b.WriteString("\t\t}\n")
		default:
			fmt.Fprintf(&b, "\t\tcfg.%s = raw\n", field)
		}
		if entry.Pattern != "" {
			fmt.Fprintf(&b, "\t\tif !pattern%s.MatchString(raw) {\n", field)
			fmt.Fprintf(&b, "\t\t\treturn cfg, fmt.Errorf(%s)\n", strconv.Quote(entry.Key+": does not match the required pattern"))
			b.WriteString("\t\t}\n")
		}
		b.WriteString("\t}\n")
	}
	required := map[string][]string{}
	for _, entry := range schema {
		for envType, value := range entry.Required {
			if value {
				required[envType] = append(required[envType], entry.Key)
			}
		}
	}
	if len(required) > 0 {
		b.WriteString("\tif envType != \"\" {\n")
		b.WriteString("\t\trequiredByEnv := map[string][]string{\n")
		envTypes := make([]string, 0, len(required))
		for envType := range required {
			envTypes = append(envTypes, envType)
		}
		sort.Strings(envTypes)
		for _, envType := range envTypes {
			keys := required[envType]
			sort.Strings(keys)
			quoted := make([]string, 0, len(keys))
			for _, key := range keys {
				quoted = append(quoted, strconv.Quote(key))
			}
			fmt.Fprintf(&b, "\t\t\t%s: {%s},\n", strconv.Quote(envType), strings.Join(quoted, ", "))
		}
		b.WriteString("\t\t}\n")
		b.WriteString("\t\tfor _, key := range requiredByEnv[envType] {\n")
		b.WriteString("\t\t\tif _, ok := values[key]; !ok {\n")
		b.WriteString("\t\t\t\treturn cfg, fmt.Errorf(\"%s: required in %s\", key, envType)\n")
		b.WriteString("\t\t\t}\n\t\t}\n\t}\n")
	}
	b.WriteString("\treturn cfg, nil\n}\n")

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", fmt.Errorf("generated Go did not parse: %w", err)
	}
	return string(formatted), nil
}

func goType(entry schemaJSON) string {
	switch schemaType(entry) {
	case "int":
		return "int"
	case "bool":
		return "bool"
	case "json":
		return "json.RawMessage"
	default:
		return "string"
	}
}

func goFieldNames(schema []schemaJSON) []string {
	used := map[string]int{}
	names := make([]string, len(schema))
	for i, entry := range schema {
		parts := strings.FieldsFunc(entry.Key, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
		var b strings.Builder
		for _, part := range parts {
			upper := strings.ToUpper(part)
			if _, ok := goInitialisms[upper]; ok {
				b.WriteString(upper)
				continue
			}
			runes := []rune(strings.ToLower(part))
			if len(runes) == 0 {
				continue
			}
			runes[0] = unicode.ToUpper(runes[0])
			b.WriteString(string(runes))
		}
		name := b.String()
		if name == "" {
			name = "Key"
		}
		if unicode.IsDigit([]rune(name)[0]) {
			name = "Key" + name
		}
		used[name]++
		if used[name] > 1 {
			name = fmt.Sprintf("%s%d", name, used[name])
		}
		names[i] = name
	}
	return names
}

var goInitialisms = map[string]struct{}{
	"ACL": {}, "API": {}, "ARN": {}, "AWS": {}, "CLI": {}, "CPU": {}, "CSV": {},
	"DB": {}, "DNS": {}, "ECS": {}, "EKS": {}, "GCP": {}, "GCS": {}, "HTTP": {},
	"HTTPS": {}, "ID": {}, "IP": {}, "JWT": {}, "KMS": {}, "MFA": {}, "OIDC": {},
	"PAT": {}, "PDF": {}, "RSA": {}, "S3": {}, "SDK": {}, "SHA": {}, "SMTP": {},
	"SQL": {}, "SSO": {}, "SSL": {}, "TLS": {}, "TTL": {}, "UI": {}, "URI": {},
	"URL": {}, "UUID": {},
}

func generatePython(schema []schemaJSON, typeName string) (string, error) {
	if typeName == "" {
		typeName = "Config"
	}
	names := pythonFieldNames(schema)
	needJSON, needURL, needLiteral := false, false, false
	for _, entry := range schema {
		switch schemaType(entry) {
		case "json":
			needJSON = true
		case "url":
			needURL = true
		case "enum":
			needLiteral = true
		}
	}
	var b strings.Builder
	b.WriteString("from __future__ import annotations\n\n")
	if needJSON {
		b.WriteString("import json\n")
	}
	b.WriteString("import os\n")
	b.WriteString("from typing import ")
	typingImports := []string{"Any", "Mapping"}
	if needLiteral {
		typingImports = append(typingImports, "Literal")
	}
	sort.Strings(typingImports)
	b.WriteString(strings.Join(typingImports, ", "))
	b.WriteString("\n\n")
	b.WriteString("from pydantic import ")
	if needURL {
		b.WriteString("AnyUrl, ")
	}
	b.WriteString("BaseModel, ConfigDict, Field, field_validator\n\n\n")
	fmt.Fprintf(&b, "class %s(BaseModel):\n", typeName)
	b.WriteString("    model_config = ConfigDict(populate_by_name=True)\n\n")

	var jsonFields []string
	for i, entry := range schema {
		required := requiredAnywhere(entry)
		fieldType := pythonType(entry)
		if !required {
			fieldType += " | None"
		}
		args := []string{}
		if !required {
			args = append(args, "default=None")
		}
		args = append(args, "alias="+jsonString(entry.Key))
		if entry.Pattern != "" && (schemaType(entry) == "string" || schemaType(entry) == "url") {
			args = append(args, "pattern="+jsonString(entry.Pattern))
		}
		fmt.Fprintf(&b, "    %s: %s = Field(%s)\n", names[i], fieldType, strings.Join(args, ", "))
		if schemaType(entry) == "json" {
			jsonFields = append(jsonFields, names[i])
		}
	}
	if len(jsonFields) > 0 {
		b.WriteString("\n    @field_validator(")
		for i, field := range jsonFields {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", field)
		}
		b.WriteString(", mode=\"before\")\n")
		b.WriteString("    @classmethod\n")
		b.WriteString("    def _parse_json_fields(cls, value: Any) -> Any:\n")
		b.WriteString("        if isinstance(value, str):\n")
		b.WriteString("            try:\n")
		b.WriteString("                return json.loads(value)\n")
		b.WriteString("            except json.JSONDecodeError as exc:\n")
		b.WriteString("                raise ValueError(\"must be valid JSON\") from exc\n")
		b.WriteString("        return value\n")
	}
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "def load_config(environ: Mapping[str, str] | None = None) -> %s:\n", typeName)
	b.WriteString("    values = dict(os.environ if environ is None else environ)\n")
	fmt.Fprintf(&b, "    return %s.model_validate(\n", typeName)
	b.WriteString("        {\n")
	for _, entry := range schema {
		fmt.Fprintf(&b, "            %s: values.get(%s),\n", jsonString(entry.Key), jsonString(entry.Key))
	}
	b.WriteString("        }\n")
	b.WriteString("    )\n")
	return b.String(), nil
}

func pythonType(entry schemaJSON) string {
	switch schemaType(entry) {
	case "url":
		return "AnyUrl"
	case "int":
		return "int"
	case "bool":
		return "bool"
	case "json":
		return "Any"
	case "enum":
		values := make([]string, 0, len(entry.Enum))
		for _, value := range entry.Enum {
			values = append(values, jsonString(value))
		}
		return "Literal[" + strings.Join(values, ", ") + "]"
	default:
		return "str"
	}
}

func pythonFieldNames(schema []schemaJSON) []string {
	used := map[string]int{}
	names := make([]string, len(schema))
	for i, entry := range schema {
		var b strings.Builder
		for _, r := range strings.ToLower(entry.Key) {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				b.WriteRune(r)
			} else {
				b.WriteRune('_')
			}
		}
		name := strings.Trim(b.String(), "_")
		if name == "" {
			name = "field"
		}
		if unicode.IsDigit([]rune(name)[0]) {
			name = "_" + name
		}
		used[name]++
		if used[name] > 1 {
			name = fmt.Sprintf("%s_%d", name, used[name])
		}
		names[i] = name
	}
	return names
}

func generateRust(schema []schemaJSON, typeName string) (string, error) {
	if typeName == "" {
		typeName = "Config"
	}
	names := rustFieldNames(schema)
	enumNames := make([]string, len(schema))
	usedTypes := map[string]int{}
	needURL, needPatterns, needJSON := false, false, false
	for i, entry := range schema {
		switch schemaType(entry) {
		case "url":
			needURL = true
		case "json":
			needJSON = true
		case "enum":
			if len(entry.Enum) == 0 {
				break
			}
			name := rustTypeName(entry.Key)
			usedTypes[name]++
			if usedTypes[name] > 1 {
				name = fmt.Sprintf("%s%d", name, usedTypes[name])
			}
			enumNames[i] = name
		}
		if entry.Pattern != "" {
			needPatterns = true
		}
	}

	var b strings.Builder
	b.WriteString("// Code generated from the Clavis project schema. DO NOT EDIT.\n\n")
	b.WriteString("use std::env;\nuse std::fmt;\n\n")
	var notes []string
	if needPatterns {
		notes = append(notes, "// Schema patterns are not checked: the standard library has no regex engine.")
	}
	if needJSON {
		notes = append(notes, "// JSON fields keep their raw text: the standard library has no JSON decoder.")
	}
	if len(notes) > 0 {
		b.WriteString("// Generated without third-party crates, so:\n")
		for _, note := range notes {
			b.WriteString(note + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("/// Typed configuration generated from the Clavis schema.\n")
	b.WriteString("#[derive(Debug, Clone, Default, PartialEq, Eq)]\n")
	fmt.Fprintf(&b, "pub struct %s {\n", typeName)
	for i, entry := range schema {
		description := strings.Join(strings.Fields(entry.Description), " ")
		if description != "" {
			fmt.Fprintf(&b, "    /// %s\n", description)
		}
		if schemaType(entry) == "enum" && len(entry.Enum) > 0 {
			fmt.Fprintf(&b, "    /// Values: %s\n", strings.Join(rustEnumValues(entry), ", "))
		}
		fmt.Fprintf(&b, "    pub %s: %s,\n", names[i], rustType(entry, enumNames[i]))
	}
	b.WriteString("}\n\n")

	for i, entry := range schema {
		if schemaType(entry) != "enum" || len(entry.Enum) == 0 {
			continue
		}
		values := rustEnumValues(entry)
		variants := rustVariantNames(values)
		enumType := enumNames[i]
		fmt.Fprintf(&b, "/// Accepted values for %s.\n", entry.Key)
		b.WriteString("#[derive(Debug, Clone, Copy, PartialEq, Eq)]\n")
		fmt.Fprintf(&b, "pub enum %s {\n", enumType)
		for _, variant := range variants {
			fmt.Fprintf(&b, "    %s,\n", variant)
		}
		b.WriteString("}\n\n")
		fmt.Fprintf(&b, "impl %s {\n", enumType)
		b.WriteString("    fn parse(value: &str) -> Option<Self> {\n")
		b.WriteString("        match value {\n")
		for j, value := range values {
			fmt.Fprintf(&b, "            %s => Some(Self::%s),\n", jsonString(value), variants[j])
		}
		b.WriteString("            _ => None,\n")
		b.WriteString("        }\n    }\n}\n\n")
		fmt.Fprintf(&b, "impl fmt::Display for %s {\n", enumType)
		b.WriteString("    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {\n")
		b.WriteString("        f.write_str(match self {\n")
		for j, value := range values {
			fmt.Fprintf(&b, "            Self::%s => %s,\n", variants[j], jsonString(value))
		}
		b.WriteString("        })\n    }\n}\n\n")
	}

	b.WriteString("/// A missing or malformed configuration value.\n")
	b.WriteString("#[derive(Debug, Clone, PartialEq, Eq)]\n")
	b.WriteString("pub struct ConfigError {\n    pub key: &'static str,\n    pub message: String,\n}\n\n")
	b.WriteString("impl ConfigError {\n")
	b.WriteString("    pub fn new(key: &'static str, message: impl Into<String>) -> Self {\n")
	b.WriteString("        Self { key, message: message.into() }\n    }\n}\n\n")
	b.WriteString("impl fmt::Display for ConfigError {\n")
	b.WriteString("    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {\n")
	b.WriteString("        write!(f, \"{}: {}\", self.key, self.message)\n    }\n}\n\n")
	b.WriteString("impl std::error::Error for ConfigError {}\n\n")

	b.WriteString("/// Load the configuration from the process environment.\n")
	fmt.Fprintf(&b, "pub fn load() -> Result<%s, ConfigError> {\n    load_for(\"\")\n}\n\n", typeName)
	b.WriteString("/// Load the configuration and enforce the keys required for `env_type`.\n")
	fmt.Fprintf(&b, "pub fn load_for(env_type: &str) -> Result<%s, ConfigError> {\n", typeName)
	b.WriteString("    let environ: Vec<(String, String)> = env::vars().collect();\n")
	b.WriteString("    load_from(&environ, env_type)\n}\n\n")
	b.WriteString("/// Load from KEY=VALUE pairs; required keys are checked when `env_type` is set.\n")
	fmt.Fprintf(&b, "pub fn load_from(environ: &[(String, String)], env_type: &str) -> Result<%s, ConfigError> {\n", typeName)
	fmt.Fprintf(&b, "    let mut cfg = %s::default();\n", typeName)
	b.WriteString("    for (key, value) in environ {\n        match key.as_str() {\n")
	for i, entry := range schema {
		target := "cfg." + names[i]
		optional := !requiredAnywhere(entry)
		fmt.Fprintf(&b, "            %s => {\n", jsonString(entry.Key))
		switch schemaType(entry) {
		case "url":
			fmt.Fprintf(&b, "                if !is_absolute_url(value) {\n")
			fmt.Fprintf(&b, "                    return Err(ConfigError::new(%s, \"must be an absolute URL\"));\n", jsonString(entry.Key))
			b.WriteString("                }\n")
			fmt.Fprintf(&b, "                %s;\n", rustAssign(target, optional, "value.clone()"))
		case "int":
			fmt.Fprintf(&b, "                let parsed = value.parse::<i64>().map_err(|_| ConfigError::new(%s, \"must be an integer\"))?;\n", jsonString(entry.Key))
			fmt.Fprintf(&b, "                %s;\n", rustAssign(target, optional, "parsed"))
		case "bool":
			b.WriteString("                match value.to_ascii_lowercase().as_str() {\n")
			fmt.Fprintf(&b, "                    \"true\" | \"1\" | \"yes\" => %s,\n", rustAssign(target, optional, "true"))
			fmt.Fprintf(&b, "                    \"false\" | \"0\" | \"no\" => %s,\n", rustAssign(target, optional, "false"))
			fmt.Fprintf(&b, "                    _ => return Err(ConfigError::new(%s, \"must be a boolean\")),\n", jsonString(entry.Key))
			b.WriteString("                }\n")
		case "enum":
			if len(entry.Enum) == 0 {
				fmt.Fprintf(&b, "                %s;\n", rustAssign(target, optional, "value.clone()"))
				break
			}
			fmt.Fprintf(&b, "                let parsed = %s::parse(value).ok_or_else(|| ConfigError::new(%s, \"must be one of [%s]\"))?;\n",
				enumNames[i], jsonString(entry.Key), strings.Join(rustEnumValues(entry), ", "))
			fmt.Fprintf(&b, "                %s;\n", rustAssign(target, optional, "parsed"))
		default:
			fmt.Fprintf(&b, "                %s;\n", rustAssign(target, optional, "value.clone()"))
		}
		b.WriteString("            }\n")
	}
	b.WriteString("            _ => {}\n        }\n    }\n")

	required := map[string][]string{}
	for _, entry := range schema {
		for envType, value := range entry.Required {
			if value {
				required[envType] = append(required[envType], entry.Key)
			}
		}
	}
	if len(required) > 0 {
		envTypes := make([]string, 0, len(required))
		for envType := range required {
			envTypes = append(envTypes, envType)
		}
		sort.Strings(envTypes)
		b.WriteString("    let required: &[&str] = match env_type {\n")
		for _, envType := range envTypes {
			keys := required[envType]
			sort.Strings(keys)
			quoted := make([]string, 0, len(keys))
			for _, key := range keys {
				quoted = append(quoted, jsonString(key))
			}
			fmt.Fprintf(&b, "        %s => &[%s],\n", jsonString(envType), strings.Join(quoted, ", "))
		}
		b.WriteString("        _ => &[],\n    };\n")
		b.WriteString("    for &key in required {\n")
		b.WriteString("        if !environ.iter().any(|(name, _)| name.as_str() == key) {\n")
		b.WriteString("            return Err(ConfigError::new(key, format!(\"required in {}\", env_type)));\n")
		b.WriteString("        }\n    }\n")
	} else {
		b.WriteString("    let _ = env_type;\n")
	}

	b.WriteString("    Ok(cfg)\n}\n")
	if needURL {
		b.WriteString("\nfn is_absolute_url(value: &str) -> bool {\n")
		b.WriteString("    match value.split_once(\"://\") {\n")
		b.WriteString("        Some((scheme, rest)) => {\n")
		b.WriteString("            !rest.is_empty()\n")
		b.WriteString("                && scheme.chars().next().map_or(false, |c| c.is_ascii_alphabetic())\n")
		b.WriteString("                && scheme\n")
		b.WriteString("                    .chars()\n")
		b.WriteString("                    .all(|c| c.is_ascii_alphanumeric() || matches!(c, '+' | '-' | '.'))\n")
		b.WriteString("        }\n        None => false,\n    }\n}\n")
	}
	return b.String(), nil
}

func rustAssign(target string, optional bool, expression string) string {
	if optional {
		return fmt.Sprintf("%s = Some(%s)", target, expression)
	}
	return fmt.Sprintf("%s = %s", target, expression)
}

func rustType(entry schemaJSON, enumName string) string {
	var base string
	switch schemaType(entry) {
	case "int":
		base = "i64"
	case "bool":
		base = "bool"
	case "enum":
		if len(entry.Enum) > 0 {
			return "Option<" + enumName + ">"
		}
		base = "String"
	default:
		base = "String"
	}
	if requiredAnywhere(entry) {
		return base
	}
	return "Option<" + base + ">"
}

// rustEnumValues returns the enum values in order without duplicates, which
// would produce unreachable match arms.
func rustEnumValues(entry schemaJSON) []string {
	seen := map[string]struct{}{}
	values := make([]string, 0, len(entry.Enum))
	for _, value := range entry.Enum {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func rustFieldNames(schema []schemaJSON) []string {
	used := map[string]int{}
	names := make([]string, len(schema))
	for i, entry := range schema {
		var b strings.Builder
		for _, r := range strings.ToLower(entry.Key) {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				b.WriteRune(r)
			} else {
				b.WriteRune('_')
			}
		}
		name := strings.Trim(b.String(), "_")
		if name == "" {
			name = "field"
		}
		if unicode.IsDigit([]rune(name)[0]) {
			name = "_" + name
		}
		if _, ok := rustKeywords[name]; ok {
			name += "_"
		}
		used[name]++
		if used[name] > 1 {
			name = fmt.Sprintf("%s_%d", name, used[name])
		}
		names[i] = name
	}
	return names
}

func rustTypeName(key string) string {
	return rustPascalCase(key, "Value")
}

func rustVariantNames(values []string) []string {
	used := map[string]int{}
	names := make([]string, len(values))
	for i, value := range values {
		name := rustPascalCase(value, "Value")
		used[name]++
		if used[name] > 1 {
			name = fmt.Sprintf("%s%d", name, used[name])
		}
		names[i] = name
	}
	return names
}

func rustPascalCase(value, fallback string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var b strings.Builder
	for _, part := range parts {
		runes := []rune(strings.ToLower(part))
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}
	name := b.String()
	if name == "" {
		return fallback
	}
	if unicode.IsDigit([]rune(name)[0]) {
		name = "V" + name
	}
	return name
}

var rustKeywords = map[string]struct{}{
	"as": {}, "async": {}, "await": {}, "box": {}, "break": {}, "const": {}, "continue": {},
	"crate": {}, "dyn": {}, "else": {}, "enum": {}, "extern": {}, "false": {}, "fn": {}, "for": {},
	"if": {}, "impl": {}, "in": {}, "let": {}, "loop": {}, "match": {}, "mod": {}, "move": {},
	"mut": {}, "pub": {}, "ref": {}, "return": {}, "self": {}, "static": {}, "struct": {},
	"super": {}, "trait": {}, "true": {}, "type": {}, "union": {}, "unsafe": {}, "use": {},
	"where": {}, "while": {}, "yield": {},
}

func lowerFirst(name string) string {
	if name == "" {
		return name
	}
	runes := []rune(name)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func jsonString(value string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return strconv.Quote(value)
	}
	return string(raw)
}
