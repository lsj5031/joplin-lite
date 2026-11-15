package models_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lsj5031/joplin-lite/internal/models"
	_ "modernc.org/sqlite"
)

type officialItem struct {
	Columns []struct {
		Name    string  `json:"name"`
		Type    string  `json:"type"`
		NotNull int     `json:"notnull"`
		Default *string `json:"dflt_value"`
	} `json:"columns"`
	Text           string         `json:"text"`
	Parsed         map[string]any `json:"parsed"`
	MalformedError string         `json:"malformedError"`
}

func officialItems(t *testing.T) map[string]officialItem {
	t.Helper()
	data, err := os.ReadFile("testdata/official-envelope/items.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Items map[string]officialItem `json:"items"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture.Items
}

func TestEncryptedEnvelopeOfficialProducerAndSQLiteImport(t *testing.T) {
	for kind, fixture := range officialItems(t) {
		t.Run(kind, func(t *testing.T) {
			reduced := map[string]string{
				"id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "type_": kind,
				"encryption_applied": "1", "encryption_cipher_text": "synthetic-ciphertext",
			}
			// Use the exact timestamp supplied to the independent official generator.
			reduced["updated_time"] = models.FmtTimeForSync(1772000000000)
			got := models.SerializeEncryptedEnvelope(reduced)
			if got != fixture.Text {
				t.Fatalf("not official producer output:\n%s\nwant:\n%s", got, fixture.Text)
			}
			// The old implementation wrote exactly the present metadata keys.
			var old []string
			for _, k := range []string{"id", "updated_time", "encryption_cipher_text", "encryption_applied", "type_"} {
				old = append(old, k+": "+reduced[k])
			}
			if !strings.Contains(fixture.MalformedError, "NOT NULL constraint failed") {
				t.Fatal("official negative control missing")
			}
			for _, input := range []struct {
				name string
				text string
				fail bool
			}{{"old", strings.Join(old, "\n"), true}, {"fixed", got, false}} {
				t.Run(input.name, func(t *testing.T) {
					db, err := sql.Open("sqlite", ":memory:")
					if err != nil {
						t.Fatal(err)
					}
					defer db.Close()
					var declarations []string
					for _, c := range fixture.Columns {
						d := `"` + c.Name + `" ` + c.Type
						if c.NotNull != 0 {
							d += " NOT NULL"
						}
						if c.Default != nil {
							d += " DEFAULT " + *c.Default
						}
						declarations = append(declarations, d)
					}
					if _, err := db.Exec("CREATE TABLE items (" + strings.Join(declarations, ",") + ")"); err != nil {
						t.Fatal(err)
					}
					// Reproduce sync's update-local path, where the existing
					// note already has required values. An INSERT may fail on
					// another missing column first (or assign fresh timestamps).
					var seedNames, seedSlots []string
					var seedArgs []any
					for k, v := range fixture.Parsed {
						if k == "type_" {
							continue
						}
						seedNames = append(seedNames, `"`+k+`"`)
						seedSlots = append(seedSlots, "?")
						seedArgs = append(seedArgs, v)
					}
					if _, err := db.Exec("INSERT INTO items ("+strings.Join(seedNames, ",")+") VALUES ("+strings.Join(seedSlots, ",")+")", seedArgs...); err != nil {
						t.Fatal(err)
					}
					// Import adapter deliberately does not use Lite's parser.
					// It models the official field conversion and Synchronizer
					// fallback, pinned by generated official parser results.
					values := map[string]any{}
					if kind == "1" {
						values["body"] = ""
					}
					for _, line := range strings.Split(input.text, "\n") {
						k, v, ok := strings.Cut(line, ": ")
						if !ok || k == "type_" {
							continue
						}
						for _, c := range fixture.Columns {
							if c.Name != k {
								continue
							}
							var value any = v
							if strings.HasSuffix(k, "_time") && (k == "created_time" || k == "updated_time" || strings.HasPrefix(k, "user_")) {
								value = int64(0)
								if v != "" {
									date, err := time.Parse(time.RFC3339Nano, v)
									if err != nil {
										t.Fatal(err)
									}
									value = date.UnixMilli()
								}
							} else if c.Type != "TEXT" {
								n, err := strconv.ParseFloat(v, 64)
								if v != "" && err != nil {
									t.Fatal(err)
								}
								value = n
								if k == "latitude" || k == "longitude" {
									value = fmt.Sprintf("%.8f", n)
								} else if k == "altitude" {
									value = fmt.Sprintf("%.4f", n)
								}
							}
							values[k] = value
						}
					}
					for _, pair := range [][2]string{{"user_created_time", "created_time"}, {"user_updated_time", "updated_time"}} {
						if values[pair[0]] == nil || values[pair[0]] == int64(0) {
							values[pair[0]] = values[pair[1]]
						}
					}
					if !input.fail {
						for k, want := range fixture.Parsed {
							if k == "type_" {
								continue
							}
							match := fmt.Sprint(values[k]) == fmt.Sprint(want)
							if n, ok := want.(float64); ok {
								switch v := values[k].(type) {
								case int64:
									match = float64(v) == n
								case float64:
									match = v == n
								}
							}
							if !match {
								t.Fatalf("official parser discrepancy %s: %v != %v", k, values[k], want)
							}
						}
					}
					var assignments []string
					var args []any
					for k, v := range values {
						assignments = append(assignments, `"`+k+`" = ?`)
						args = append(args, v)
					}
					_, err = db.Exec("UPDATE items SET "+strings.Join(assignments, ","), args...)
					if input.fail {
						if err == nil || !strings.Contains(err.Error(), "NOT NULL constraint failed: items.user_created_time") {
							t.Fatalf("old envelope should reproduce import failure: %v", err)
						}
					} else if err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}
