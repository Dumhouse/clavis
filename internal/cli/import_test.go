package cli

import (
	"reflect"
	"testing"
)

func TestParseDotenv(t *testing.T) {
	input := "\uFEFF# comment\n\nDATABASE_URL=postgres://localhost/db\nexport API_KEY=\"abc\\n123\"\nSINGLE='a # b'\nEMPTY=\nINLINE=value # trailing\nHASH_NOT_COMMENT=abc#def\nESCAPES=\"quote: \\\" back: \\\\ tab:\\t\"\n"
	got, err := ParseDotenv([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"DATABASE_URL":     "postgres://localhost/db",
		"API_KEY":          "abc\n123",
		"SINGLE":           "a # b",
		"EMPTY":            "",
		"INLINE":           "value",
		"HASH_NOT_COMMENT": "abc#def",
		"ESCAPES":          "quote: \" back: \\ tab:\t",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestParseDotenvErrors(t *testing.T) {
	cases := []string{
		"NO_EQUALS\n",
		"=value\n",
		`A="unterminated` + "\n",
		"A='unterminated\n",
		`A="trailing\` + "\n",
	}
	for _, input := range cases {
		if _, err := ParseDotenv([]byte(input)); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestParseImportFile(t *testing.T) {
	jsonValues, err := parseImportFile("x.json", []byte(`{"A":"one","B":"two"}`))
	if err != nil || jsonValues["A"] != "one" {
		t.Fatalf("json: %v %v", jsonValues, err)
	}
	yamlValues, err := parseImportFile("x.yaml", []byte("A: one\nB: two\n"))
	if err != nil || yamlValues["B"] != "two" {
		t.Fatalf("yaml: %v %v", yamlValues, err)
	}
	dotenvValues, err := parseImportFile("x.env", []byte("A=one\n"))
	if err != nil || dotenvValues["A"] != "one" {
		t.Fatalf("dotenv: %v %v", dotenvValues, err)
	}
	if _, err := parseImportFile("x.json", []byte("nope")); err == nil {
		t.Fatal("bad JSON accepted")
	}
	if _, err := parseImportFile("x.yaml", []byte(":\n  - no")); err == nil {
		t.Fatal("bad YAML accepted")
	}
}

func FuzzParseDotenv(f *testing.F) {
	seeds := []string{
		"",
		"A=1\n",
		`A="x"`,
		"export A='y'\n# c\n",
		"A=a b c # d\n",
		"A=\"\\n\\t\"\n",
		"=bad\n",
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = ParseDotenv(data)
	})
}
