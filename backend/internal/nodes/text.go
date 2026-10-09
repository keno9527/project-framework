// Package nodes provides statically registered example nodes.
package nodes

import (
	"context"
	"fmt"
	"strings"

	"project-framework/internal/workflow"
)

type textValue struct {
	Text string `json:"text"`
}

type trimConfig struct {
	Trim bool `json:"trim"`
}

type emptyConfig struct{}

type textPair struct {
	Upper string `json:"upper"`
	Lower string `json:"lower"`
}

// Register installs pure text nodes and the offline order investigation sample.
// Production integrations should inject their clients into node constructors.
func Register(registry *workflow.Registry) error {
	textSchema := objectSchema(map[string]any{"text": workflow.Schema{"type": "string"}}, "text")
	pairSchema := objectSchema(map[string]any{
		"upper": workflow.Schema{"type": "string"},
		"lower": workflow.Schema{"type": "string"},
	}, "upper", "lower")
	registrations := []workflow.Registration{
		{
			Descriptor: descriptor("text.normalize", "清理文本", "按配置去除文本首尾空白。", "文本处理",
				objectSchema(map[string]any{"trim": workflow.Schema{"type": "boolean", "description": "是否去除首尾空白"}}, "trim"), textSchema, textSchema),
			Handler: workflow.Adapt(func(ctx context.Context, in textValue, cfg trimConfig, _ workflow.ExecutionMeta) (textValue, error) {
				if cfg.Trim {
					in.Text = strings.TrimSpace(in.Text)
				}
				return in, ctx.Err()
			}),
		},
		{
			Descriptor: descriptor("text.upper", "转换大写", "将文本转换为 Unicode 大写。", "文本处理", objectSchema(nil), textSchema, textSchema),
			Handler: workflow.Adapt(func(ctx context.Context, in textValue, _ emptyConfig, _ workflow.ExecutionMeta) (textValue, error) {
				return textValue{Text: strings.ToUpper(in.Text)}, ctx.Err()
			}),
		},
		{
			Descriptor: descriptor("text.lower", "转换小写", "将文本转换为 Unicode 小写。", "文本处理", objectSchema(nil), textSchema, textSchema),
			Handler: workflow.Adapt(func(ctx context.Context, in textValue, _ emptyConfig, _ workflow.ExecutionMeta) (textValue, error) {
				return textValue{Text: strings.ToLower(in.Text)}, ctx.Err()
			}),
		},
		{
			Descriptor: descriptor("text.merge", "汇合文本", "汇合两个独立分支的文本结果。", "文本处理", objectSchema(nil), pairSchema, pairSchema),
			Handler: workflow.Adapt(func(ctx context.Context, in textPair, _ emptyConfig, _ workflow.ExecutionMeta) (textPair, error) {
				return in, ctx.Err()
			}),
		},
	}
	registrations = append(registrations, orderRegistrations()...)
	for _, registration := range registrations {
		if err := registry.Register(registration); err != nil {
			return fmt.Errorf("register example node %s: %w", registration.Descriptor.Type, err)
		}
	}
	return nil
}

func objectSchema(properties map[string]any, required ...string) workflow.Schema {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := workflow.Schema{"type": "object", "additionalProperties": false, "properties": properties}
	if len(required) != 0 {
		schema["required"] = required
	}
	return schema
}

func descriptor(nodeType, title, description, category string, config, input, output workflow.Schema) workflow.Descriptor {
	return workflow.Descriptor{
		Type: nodeType, TypeVersion: "1", Title: title, Description: description,
		Category: category, ConfigSchema: config, InputSchema: input, OutputSchema: output, RetrySafe: true,
	}
}
