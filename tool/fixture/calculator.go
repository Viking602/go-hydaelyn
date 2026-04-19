package fixture

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn/tool"
)

type CalculatorTool struct{}

type calculatorInput struct {
	Operation string    `json:"operation"`
	Operands  []float64 `json:"operands"`
}

type calculatorOutput struct {
	Result float64 `json:"result"`
}

func NewCalculatorTool() *CalculatorTool {
	return &CalculatorTool{}
}

func (t *CalculatorTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "calculator",
		Description: "Perform deterministic arithmetic",
		InputSchema: tool.Schema{
			Type: "object",
			Properties: map[string]tool.Schema{
				"operation": {Type: "string"},
				"operands":  {Type: "array", Items: &tool.Schema{Type: "number"}},
			},
			Required: []string{"operation", "operands"},
		},
	}
}

func (t *CalculatorTool) Execute(_ context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var input calculatorInput
	if err := decodeArgs(call, &input); err != nil {
		return tool.Result{}, err
	}
	if len(input.Operands) == 0 {
		return tool.Result{}, fmt.Errorf("operands are required")
	}
	result := input.Operands[0]
	switch input.Operation {
	case "add":
		result = 0
		for _, operand := range input.Operands {
			result += operand
		}
	case "subtract":
		for _, operand := range input.Operands[1:] {
			result -= operand
		}
	case "multiply":
		for _, operand := range input.Operands[1:] {
			result *= operand
		}
	case "divide":
		for _, operand := range input.Operands[1:] {
			if operand == 0 {
				return tool.Result{}, fmt.Errorf("division by zero")
			}
			result /= operand
		}
	default:
		return tool.Result{}, fmt.Errorf("unsupported operation %q", input.Operation)
	}
	return jsonResult(call, t.Definition().Name, calculatorOutput{Result: result})
}
