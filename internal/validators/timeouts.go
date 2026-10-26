package validators

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// PositiveDuration validates explicit timeout values before an operation begins.
func PositiveDuration() validator.String {
	return stringCheck{"must be a positive duration such as 30s or 20m", func(s string) bool { d, err := time.ParseDuration(s); return err == nil && d > 0 }}
}

// TimeoutsBlock retains the framework-timeouts custom value type while adding
// positive duration validation and Markdown descriptions to its optional block.
func TimeoutsBlock(ctx context.Context, opts timeouts.Opts) schema.Block {
	block := timeouts.Block(ctx, opts).(schema.SingleNestedBlock)
	block.MarkdownDescription = "Operation timeouts. One timeout covers lock acquisition, API requests, queued work and readiness polling."
	for name, attribute := range block.Attributes {
		value := attribute.(schema.StringAttribute)
		value.MarkdownDescription = value.Description
		value.Validators = append(value.Validators, PositiveDuration())
		block.Attributes[name] = value
	}
	return block
}
