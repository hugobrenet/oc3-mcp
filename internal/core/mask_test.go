package core

import (
	"strings"
	"testing"
)

func TestMaskServiceConfig(t *testing.T) {
	text := `[DEFAULT]
nodes = n1 n2
# contact ops@example.com
schedule@n1.example.com = @1d

[env]
DB_PASSWORD = s3cret
db_password@n1 = n1-secret
api_token =
    first-token-line
    second-token-line
admin = bob@example.com
secrets_environment = DB_PASSWORD=app/db_password
configs_environment = CONF=app/conf

[fs#1]
install =
 /etc/app from ./cfg/{name}
`
	masked, truncated := maskServiceConfig(text)
	for _, secret := range []string{"s3cret", "n1-secret", "first-token-line", "second-token-line", "ops@example.com", "bob@example.com"} {
		if strings.Contains(masked, secret) {
			t.Fatalf("%q left in:\n%s", secret, masked)
		}
	}
	for _, kept := range []string{"schedule@n1.example.com = @1d", "secrets_environment = DB_PASSWORD=app/db_password", "configs_environment = CONF=app/conf",
		" /etc/app from ./cfg/{name}", "nodes = n1 n2", "admin = (masked)"} {
		if !strings.Contains(masked, kept) {
			t.Fatalf("%q missing in:\n%s", kept, masked)
		}
	}
	if truncated {
		t.Fatal("a short text was cut")
	}
	if long, cut := maskServiceConfig(strings.Repeat("a = b\n", 10000)); !cut || len(long) > maxServiceConfig {
		t.Fatalf("%d bytes, cut %v", len(long), cut)
	}
}
