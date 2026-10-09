package nodes

import (
	"context"
	"strings"

	"project-framework/internal/workflow"
)

type orderInput struct {
	OrderID string `json:"orderId"`
}

type serviceResult struct {
	Found  bool   `json:"found"`
	Status string `json:"status"`
}

type aggregateInput struct {
	Order    serviceResult `json:"order"`
	Delivery serviceResult `json:"delivery"`
}

type aggregateResult struct {
	Consistent bool     `json:"consistent"`
	Issues     []string `json:"issues"`
}

// orderRegistrations uses deterministic in-memory fixtures, not live services.
// A negative business conclusion is valid output, not a workflow failure.
func orderRegistrations() []workflow.Registration {
	idSchema := objectSchema(map[string]any{
		"orderId": workflow.Schema{"type": "string", "minLength": 1, "description": "demo-1001、demo-1002 或任意不存在的订单"},
	}, "orderId")
	serviceSchema := objectSchema(map[string]any{
		"found": workflow.Schema{"type": "boolean"}, "status": workflow.Schema{"type": "string"},
	}, "found", "status")
	orderOutput := objectSchema(map[string]any{"order": serviceSchema}, "order")
	deliveryOutput := objectSchema(map[string]any{"delivery": serviceSchema}, "delivery")
	aggregateSchema := objectSchema(map[string]any{"order": serviceSchema, "delivery": serviceSchema}, "order", "delivery")
	resultSchema := objectSchema(map[string]any{
		"consistent": workflow.Schema{"type": "boolean"},
		"issues":     workflow.Schema{"type": "array", "items": workflow.Schema{"type": "string"}},
	}, "consistent", "issues")
	const category = "订单排查 · 内置样例"
	return []workflow.Registration{
		{
			Descriptor: descriptor("demo.order.validate", "规范订单标识", "处理用于离线排查的测试订单标识。", category, objectSchema(nil), idSchema, idSchema),
			Handler: workflow.Adapt(func(ctx context.Context, in orderInput, _ emptyConfig, _ workflow.ExecutionMeta) (orderInput, error) {
				return orderInput{OrderID: strings.TrimSpace(in.OrderID)}, ctx.Err()
			}),
		},
		{
			Descriptor: descriptor("demo.order.read", "读取订单", "读取内置测试快照，不访问真实订单系统。", category, objectSchema(nil), idSchema, orderOutput),
			Handler: workflow.Adapt(func(ctx context.Context, in orderInput, _ emptyConfig, _ workflow.ExecutionMeta) (struct {
				Order serviceResult `json:"order"`
			}, error) {
				result := serviceResult{Found: in.OrderID == "demo-1001" || in.OrderID == "demo-1002"}
				if result.Found {
					result.Status = "shipped"
				}
				return struct {
					Order serviceResult `json:"order"`
				}{Order: result}, ctx.Err()
			}),
		},
		{
			Descriptor: descriptor("demo.delivery.read", "读取配送", "读取内置配送快照；demo-1001 的状态与订单不一致。", category, objectSchema(nil), idSchema, deliveryOutput),
			Handler: workflow.Adapt(func(ctx context.Context, in orderInput, _ emptyConfig, _ workflow.ExecutionMeta) (struct {
				Delivery serviceResult `json:"delivery"`
			}, error) {
				result := serviceResult{}
				switch in.OrderID {
				case "demo-1001":
					result = serviceResult{Found: true, Status: "pending"}
				case "demo-1002":
					result = serviceResult{Found: true, Status: "in_transit"}
				}
				return struct {
					Delivery serviceResult `json:"delivery"`
				}{Delivery: result}, ctx.Err()
			}),
		},
		{
			Descriptor: descriptor("demo.order.aggregate", "汇合排查结论", "汇总订单与配送快照；发现不一致时返回业务问题，执行仍可成功。", category, objectSchema(nil), aggregateSchema, resultSchema),
			Handler: workflow.Adapt(func(ctx context.Context, in aggregateInput, _ emptyConfig, _ workflow.ExecutionMeta) (aggregateResult, error) {
				out := aggregateResult{Issues: []string{}}
				if !in.Order.Found {
					out.Issues = append(out.Issues, "订单不存在")
				}
				if !in.Delivery.Found {
					out.Issues = append(out.Issues, "配送记录不存在")
				}
				if in.Order.Found && in.Delivery.Found && in.Order.Status == "shipped" && in.Delivery.Status != "in_transit" {
					out.Issues = append(out.Issues, "订单已发货，但配送尚未进入运输状态")
				}
				out.Consistent = len(out.Issues) == 0
				return out, ctx.Err()
			}),
		},
	}
}
