package logger

import (
	"testing"
)

func TestRedactFieldsPassword(t *testing.T) {
	fields := map[string]any{
		"username": "admin",
		"password": "secret123",
	}
	result := RedactFields(fields)

	if result["username"] != "admin" {
		t.Errorf("username should not be redacted, got %v", result["username"])
	}
	if result["password"] != "<redacted>" {
		t.Errorf("password should be redacted, got %v", result["password"])
	}
}

func TestRedactFieldsToken(t *testing.T) {
	fields := map[string]any{
		"token":    "abc123",
		"api_key":  "key-xyz",
		"name":     "test",
	}
	result := RedactFields(fields)

	if result["token"] != "<redacted>" {
		t.Errorf("token should be redacted")
	}
	if result["api_key"] != "<redacted>" {
		t.Errorf("api_key should be redacted")
	}
	if result["name"] != "test" {
		t.Errorf("name should not be redacted")
	}
}

func TestRedactFieldsPEM(t *testing.T) {
	fields := map[string]any{
		"cert": "-----BEGIN PRIVATE KEY-----\nMIIEvgIBADANBgkq\n-----END PRIVATE KEY-----",
	}
	result := RedactFields(fields)

	if result["cert"] != "<redacted>" {
		t.Errorf("PEM private key should be redacted, got %v", result["cert"])
	}
}

func TestRedactFieldsNested(t *testing.T) {
	fields := map[string]any{
		"config": map[string]any{
			"host":     "localhost",
			"password": "nested-secret",
		},
	}
	result := RedactFields(fields)

	config := result["config"].(map[string]any)
	if config["host"] != "localhost" {
		t.Errorf("nested host should not be redacted")
	}
	if config["password"] != "<redacted>" {
		t.Errorf("nested password should be redacted")
	}
}

func TestRedactFieldsNil(t *testing.T) {
	result := RedactFields(nil)
	if result != nil {
		t.Errorf("nil input should return nil, got %v", result)
	}
}

func TestRedactFieldsCaseInsensitive(t *testing.T) {
	fields := map[string]any{
		"Password":  "secret",
		"API_KEY":   "key",
		"Token":     "tok",
	}
	result := RedactFields(fields)

	if result["Password"] != "<redacted>" {
		t.Errorf("Password (capitalized) should be redacted")
	}
	if result["API_KEY"] != "<redacted>" {
		t.Errorf("API_KEY (uppercase) should be redacted")
	}
	if result["Token"] != "<redacted>" {
		t.Errorf("Token (capitalized) should be redacted")
	}
}

func TestRedactFieldsBearerToken(t *testing.T) {
	fields := map[string]any{
		"bearertoken": "Bearer xyz",
		"uitoken":     "ui-abc",
	}
	result := RedactFields(fields)

	if result["bearertoken"] != "<redacted>" {
		t.Errorf("bearertoken should be redacted")
	}
	if result["uitoken"] != "<redacted>" {
		t.Errorf("uitoken should be redacted")
	}
}
