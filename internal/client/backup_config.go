package client

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
)

type BackupRetentionPolicy struct {
	MaxTotalFiles int64    `json:"maxTotalFiles"`
	MaxTotalGiB   int64    `json:"maxTotalGiB"`
	ProtectedTags []string `json:"protectedTags"`
	MinDaily      int64    `json:"minDaily"`
	MaxDaily      int64    `json:"maxDaily"`
	MinWeekly     int64    `json:"minWeekly"`
	MaxWeekly     int64    `json:"maxWeekly"`
	MinMonthly    int64    `json:"minMonthly"`
	MaxMonthly    int64    `json:"maxMonthly"`
}

func (v *BackupRetentionPolicy) UnmarshalJSON(data []byte) error {
	type wire BackupRetentionPolicy
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range []string{"maxTotalFiles", "maxTotalGiB", "protectedTags", "minDaily", "maxDaily", "minWeekly", "maxWeekly", "minMonthly", "maxMonthly"} {
		value, ok := fields[key]
		if !ok || string(value) == "null" {
			return errors.New("backup retention response missing required field")
		}
	}
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*v = BackupRetentionPolicy(decoded)
	return nil
}

type BackupConfigWrite struct {
	BackupPeriod    *string                `json:"backupPeriod"`
	RetentionPolicy *BackupRetentionPolicy `json:"retentionPolicy"`
}
type BackupConfig struct {
	BackupConfigWrite
	DefaultRetentionPolicy *BackupRetentionPolicy `json:"defaultRetentionPolicy"`
}

func (v *BackupConfig) UnmarshalJSON(data []byte) error {
	type wire BackupConfig
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range []string{"backupPeriod", "retentionPolicy", "defaultRetentionPolicy"} {
		if _, ok := fields[key]; !ok {
			return errors.New("backup response missing configuration field")
		}
	}
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.DefaultRetentionPolicy == nil {
		return errors.New("backup response missing default retention policy")
	}
	*v = BackupConfig(decoded)
	return nil
}

func (c *Client) GetBackupConfig(ctx context.Context, id string) (BackupConfig, error) {
	ctx, state := c.operation(ctx, "GetBackupConfig")
	p, err := input[path.ServiceStackId](map[string]any{"Id": id})
	if err != nil {
		return BackupConfig{}, err
	}
	r, outer := c.sdk.GetServiceStackBackup(ctx, p)
	v, err := finish[BackupConfig](state, r, outer)
	if err == nil && v.DefaultRetentionPolicy == nil {
		return v, errors.New("backup response missing default retention policy")
	}
	if v.BackupPeriod != nil && *v.BackupPeriod == "" {
		v.BackupPeriod = nil
	}
	return v, err
}

type backupProcessEnvelope struct {
	Process *Process `json:"process"`
}

func (v *backupProcessEnvelope) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	value, ok := fields["process"]
	if !ok {
		return errors.New("backup write response missing process field")
	}
	if err := json.Unmarshal(value, &v.Process); err != nil {
		return err
	}
	if v.Process != nil && v.Process.ID == "" {
		return errors.New("backup write response missing process ID")
	}
	return nil
}
func (c *Client) SetBackupConfig(ctx context.Context, id string, v BackupConfigWrite) (*Process, error) {
	ctx, state := c.operation(ctx, "SetBackupConfig")
	p, err := input[path.ServiceStackId](map[string]any{"Id": id})
	if err != nil {
		return nil, err
	}
	b, err := input[body.ServiceStackBackupConfig](v)
	if err != nil {
		return nil, err
	}
	r, outer := c.sdk.PutServiceStackBackupConfig(ctx, p, b)
	value, err := finish[backupProcessEnvelope](state, r, outer)
	return normalizeProcess(value.Process), err
}
