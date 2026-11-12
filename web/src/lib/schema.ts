import type { components } from '@/api/schema'

export type SchemaEntry = components['schemas']['SchemaEntryBody']

export type SchemaDraft = {
  key: string
  type: SchemaEntry['type']
  kind: SchemaEntry['kind']
  required: Record<string, boolean>
  pattern: string
  description: string
  example: string
  enum: string[]
}

export const schemaTypes: SchemaEntry['type'][] = ['string', 'url', 'int', 'bool', 'json', 'enum']
export const schemaKinds: SchemaEntry['kind'][] = ['secret', 'variable', 'file']

export function toDraft(entry: SchemaEntry): SchemaDraft {
  return {
    key: entry.key,
    type: entry.type ?? 'string',
    kind: entry.kind ?? 'secret',
    required: { ...(entry.required ?? {}) },
    pattern: entry.pattern ?? '',
    description: entry.description ?? '',
    example: entry.example ?? '',
    enum: entry.enum ?? [],
  }
}

export function fromDraft(entry: SchemaDraft): SchemaEntry {
  return {
    key: entry.key.trim(),
    type: entry.type,
    kind: entry.kind,
    required: entry.required,
    pattern: entry.pattern,
    description: entry.description,
    example: entry.example,
    enum: entry.type === 'enum' ? entry.enum : [],
  }
}

export function isRequired(entry: SchemaEntry | undefined, envType: string): boolean {
  return Boolean(entry?.required?.[envType])
}

/** validateSchemaValue mirrors the server's schema checks for instant feedback. */
export function validateSchemaValue(entry: SchemaEntry | undefined, value: string): string | null {
  if (!entry || value === '' || entry.type === 'string') {
    if (entry?.pattern) return matchesPattern(entry, value)
    return null
  }
  switch (entry.type) {
    case 'url': {
      try {
        const parsed = new URL(value)
        if (!parsed.protocol || !parsed.host) return 'Must be an absolute URL.'
      } catch {
        return 'Must be an absolute URL.'
      }
      break
    }
    case 'int':
      if (!/^-?\d+$/.test(value.trim())) return 'Must be an integer.'
      break
    case 'bool':
      if (!/^(true|false|1|0|yes|no)$/i.test(value.trim())) return 'Must be a boolean.'
      break
    case 'json':
      try {
        JSON.parse(value)
      } catch {
        return 'Must be valid JSON.'
      }
      break
    case 'enum':
      if (!(entry.enum ?? []).includes(value)) {
        return `Must be one of ${(entry.enum ?? []).join(', ')}.`
      }
      break
  }
  return matchesPattern(entry, value)
}

function matchesPattern(entry: SchemaEntry, value: string): string | null {
  if (!entry.pattern || value === '') return null
  try {
    return new RegExp(entry.pattern).test(value) ? null : 'Does not match the required pattern.'
  } catch {
    return 'The schema pattern is not a valid regular expression.'
  }
}

export function draftError(entry: SchemaDraft, all: SchemaDraft[]): string | null {
  if (!/^[A-Za-z_][A-Za-z0-9_.-]*$/.test(entry.key.trim())) {
    return 'Key must start with a letter or underscore.'
  }
  if (all.filter((candidate) => candidate.key.trim() === entry.key.trim()).length > 1) {
    return 'Keys must be unique.'
  }
  if (entry.pattern) {
    try {
      new RegExp(entry.pattern)
    } catch {
      return 'Pattern is not a valid regular expression.'
    }
  }
  if (entry.type === 'enum') {
    if (entry.enum.length === 0) return 'Add at least one enum value.'
    if (new Set(entry.enum).size !== entry.enum.length) return 'Enum values must be unique.'
  }
  return null
}
