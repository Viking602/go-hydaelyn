package recipe

import (
	"encoding/json"
	"os"

	"gopkg.in/yaml.v3"
)

func Decode(data []byte) (Spec, error) {
	var spec Spec
	if err := yaml.Unmarshal(data, &spec); err == nil && !isEmptySpec(spec) {
		return spec, nil
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func DecodeFile(path string) (Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, err
	}
	return Decode(data)
}

func isEmptySpec(spec Spec) bool {
	return spec.Pattern == "" && len(spec.Flow) == 0 && len(spec.Tasks) == 0
}
