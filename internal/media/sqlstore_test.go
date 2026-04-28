package media

import (
	"strings"
	"testing"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
	mysql "github.com/go-sql-driver/mysql"
)

func TestOpenDriverAndDSN_SQLiteAddsLockPragmas(t *testing.T) {
	driver, dsn, err := openDriverAndDSN(config.StorageConfig{
		Engine: "sqlite",
		SQLite: config.SQLiteConfig{
			Path: t.TempDir() + "/app.db",
		},
	}, "sqlite")
	if err != nil {
		t.Fatalf("openDriverAndDSN() error = %v", err)
	}
	if driver != "sqlite" {
		t.Fatalf("driver = %q, want sqlite", driver)
	}
	for _, want := range []string{
		"_pragma=busy_timeout(10000)",
		"_pragma=journal_mode(WAL)",
		"_pragma=foreign_keys(1)",
	} {
		if !strings.Contains(dsn, want) {
			t.Fatalf("dsn = %q, want to contain %q", dsn, want)
		}
	}
}

func TestOpenDriverAndDSN_SQLiteKeepsMemoryDSN(t *testing.T) {
	_, dsn, err := openDriverAndDSN(config.StorageConfig{
		Engine: "sqlite",
		SQLite: config.SQLiteConfig{
			Path: ":memory:",
		},
	}, "sqlite")
	if err != nil {
		t.Fatalf("openDriverAndDSN() error = %v", err)
	}
	if dsn != ":memory:" {
		t.Fatalf("dsn = %q, want :memory:", dsn)
	}
}

func TestOpenDriverAndDSN_MySQLEscapesCredentialsAndPreservesParams(t *testing.T) {
	driver, dsn, err := openDriverAndDSN(config.StorageConfig{
		Engine: "mysql",
		MySQL: config.MySQLConfig{
			Host:     "::1",
			Port:     3306,
			Username: "bot",
			Password: "p@ss:/?#&=word",
			Database: "go_bot",
			Params:   "timeout=5s&parseTime=true&loc=UTC&sql_mode=ANSI_QUOTES",
		},
	}, "mysql")
	if err != nil {
		t.Fatalf("openDriverAndDSN() error = %v", err)
	}
	if driver != "mysql" {
		t.Fatalf("driver = %q, want mysql", driver)
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN(%q) error = %v", dsn, err)
	}
	if parsed.User != "bot" || parsed.Passwd != "p@ss:/?#&=word" || parsed.DBName != "go_bot" {
		t.Fatalf("parsed DSN = user %q password %q db %q, want escaped values preserved", parsed.User, parsed.Passwd, parsed.DBName)
	}
	if parsed.Addr != "[::1]:3306" {
		t.Fatalf("Addr = %q, want [::1]:3306", parsed.Addr)
	}
	if !parsed.ParseTime {
		t.Fatalf("ParseTime = false, want true")
	}
	if parsed.Loc == nil || parsed.Loc.String() != "UTC" {
		t.Fatalf("Loc = %v, want UTC", parsed.Loc)
	}
	if !strings.Contains(dsn, "charset=utf8mb4") {
		t.Fatalf("dsn = %q, want charset=utf8mb4", dsn)
	}
	if got := parsed.Params["sql_mode"]; got != "ANSI_QUOTES" {
		t.Fatalf("sql_mode param = %q, want ANSI_QUOTES", got)
	}
	if parsed.Timeout != 5*time.Second {
		t.Fatalf("Timeout = %s, want 5s", parsed.Timeout)
	}
}
