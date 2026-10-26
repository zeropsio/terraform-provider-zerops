package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

const backupPolicyJSON = `{"maxTotalFiles":10,"maxTotalGiB":2,"protectedTags":["test"],"minDaily":1,"maxDaily":3,"minWeekly":1,"maxWeekly":2,"minMonthly":1,"maxMonthly":2}`

func TestBackupConfigAdapter(t *testing.T) {
	n := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		case 1:
			if r.Method != "GET" || r.URL.Path != "/api/rest/public/service-stack/service/backup" {
				t.Fatal(r.Method, r.URL.Path)
			}
			respond(w, 200, `{"backupPeriod":"","retentionPolicy":null,"defaultRetentionPolicy":`+backupPolicyJSON+`}`)
		case 2, 3:
			if r.Method != "PUT" || r.URL.Path != "/api/rest/public/service-stack/service/backup-config" {
				t.Fatal(r.Method, r.URL.Path)
			}
			var body map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Fatal("invalid JSON")
			}
			if len(body) != 2 || string(body["backupPeriod"]) != "null" {
				t.Fatal("replacement must send both fields", body)
			}
			if n == 2 && string(body["retentionPolicy"]) == "null" {
				t.Fatal("lost retention policy")
			}
			if n == 3 && string(body["retentionPolicy"]) != "null" {
				t.Fatal("reset must clear policy")
			}
			respond(w, 200, `{"process":null}`)
		default:
			t.Fatal("mutation replayed")
		}
	})
	v, e := c.GetBackupConfig(context.Background(), "service")
	if e != nil || v.BackupPeriod != nil || v.RetentionPolicy != nil || v.DefaultRetentionPolicy.MaxTotalFiles != 10 {
		t.Fatalf("%+v %v", v, e)
	}
	var p BackupRetentionPolicy
	if e = json.Unmarshal([]byte(backupPolicyJSON), &p); e != nil {
		t.Fatal(e)
	}
	process, e := c.SetBackupConfig(context.Background(), "service", BackupConfigWrite{RetentionPolicy: &p})
	if e != nil || process != nil {
		t.Fatal(process, e)
	}
	process, e = c.SetBackupConfig(context.Background(), "service", BackupConfigWrite{})
	if e != nil || process != nil {
		t.Fatal(process, e)
	}
}
func TestBackupConfigErrors(t *testing.T) {
	for _, code := range []string{"backupUnsupportedStackType", "invalidUserInput"} {
		t.Run(code, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) { respond(w, 400, `{"error":{"code":"`+code+`"}}`) })
			_, e := c.SetBackupConfig(context.Background(), "service", BackupConfigWrite{})
			api, ok := AsAPIError(e)
			if !ok || api.Code != code {
				t.Fatal(e)
			}
		})
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { respond(w, 200, `{}`) })
	if _, e := c.GetBackupConfig(context.Background(), "service"); e == nil {
		t.Fatal("missing defaults accepted")
	}
}
