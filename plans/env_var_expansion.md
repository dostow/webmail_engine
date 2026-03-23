# Environment Variable Expansion Plan

## Overview

Add environment variable expansion support to `config.json` to allow sensitive values (DSNs, secrets) to be externalized from the config file.

## Syntax

Support two syntaxes (Go's `os.ExpandEnv`):

1. **Simple**: `${VAR_NAME}` or `$VAR_NAME`
2. **With default**: `${VAR_NAME:-default_value}`

Example:
```json
{
  "store": {
    "sql": {
      "dsn": "${DATABASE_URL}"
    }
  },
  "security": {
    "encryption_key": "${ENCRYPTION_KEY:-fallback_key}"
  }
}
```

## Implementation Steps

### 1. Add `expandEnvVars` helper function

Create a recursive function that walks through the config struct and expands env vars in all string fields.

```go
// expandEnvVars recursively expands environment variables in all string fields
func expandEnvVars(cfg interface{}) {
    v := reflect.ValueOf(cfg)
    if v.Kind() != reflect.Ptr || v.IsNil() {
        return
    }
    expandEnvVarsRecursive(v.Elem())
}

func expandEnvVarsRecursive(v reflect.Value) {
    switch v.Kind() {
    case reflect.String:
        if v.CanSet() {
            v.SetString(os.ExpandEnv(v.String()))
        }
    case reflect.Struct:
        for i := 0; i < v.NumField(); i++ {
            expandEnvVarsRecursive(v.Field(i))
        }
    case reflect.Ptr:
        if !v.IsNil() {
            expandEnvVarsRecursive(v.Elem())
        }
    case reflect.Slice:
        for i := 0; i < v.Len(); i++ {
            expandEnvVarsRecursive(v.Index(i))
        }
    case reflect.Map:
        for _, key := range v.MapKeys() {
            expandEnvVarsRecursive(v.MapIndex(key))
        }
    }
}
```

### 2. Update `LoadFromFile`

Call `expandEnvVars` after unmarshaling JSON:

```go
func LoadFromFile(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("failed to read config file: %w", err)
    }

    config := DefaultConfig()
    if err := json.Unmarshal(data, config); err != nil {
        return nil, fmt.Errorf("failed to parse config file: %w", err)
    }

    // Expand environment variables
    expandEnvVars(config)

    return config, nil
}
```

### 3. Add tests

Create `internal/config/config_test.go`:

```go
func TestExpandEnvVars(t *testing.T) {
    // Test string expansion
    // Test nested structs
    // Test slices
    // Test maps
    // Test default values
}

func TestLoadFromFileWithEnvVars(t *testing.T) {
    // Test loading config with env var placeholders
}
```

### 4. Update config.json

Replace sensitive values with env var placeholders:

```json
{
  "store": {
    "type": "sql",
    "sql": {
      "driver": "postgres",
      "dsn": "${DATABASE_URL}",
      "max_connections": 10,
      "min_idle": 2
    }
  },
  "security": {
    "encryption_key": "${ENCRYPTION_KEY}",
    "webhook_secret": "${WEBHOOK_SECRET}",
    "signed_url_secret": "${SIGNED_URL_SECRET}"
  },
  "redis": {
    "password": "${REDIS_PASSWORD:-}"
  }
}
```

### 5. Update .env.example

Add all required environment variables:

```bash
# Database
DATABASE_URL=postgres://user:pass@host:port/dbname?sslmode=require

# Security
ENCRYPTION_KEY=<64-char-hex-key>
WEBHOOK_SECRET=<random-secret>
SIGNED_URL_SECRET=<random-secret>

# Redis
REDIS_PASSWORD=

# Server
SERVER_HOST=0.0.0.0
SERVER_PORT=8080
```

## Files to Modify

| File | Changes |
|------|---------|
| `internal/config/config.go` | Add `expandEnvVars`, `expandEnvVarsRecursive` functions; update `LoadFromFile` |
| `internal/config/config_test.go` | Add tests (new file) |
| `config.json` | Replace sensitive values with `${VAR}` placeholders |
| `.env.example` | Add all environment variables |

## Security Considerations

1. **Never log expanded values** - especially DSNs and secrets
2. **Validate required vars** - add validation for empty env vars after expansion
3. **Document defaults** - clearly document which vars support defaults with `:-`

## Testing

```bash
# Test with env vars set
export DATABASE_URL="postgres://..."
export ENCRYPTION_KEY="..."
go test ./internal/config/...

# Test with missing vars (should use defaults or fail validation)
unset DATABASE_URL
go test ./internal/config/...
```
