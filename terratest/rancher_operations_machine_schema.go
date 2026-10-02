package test

import (
	"errors"
	"fmt"
	"strings"
)

var errDeployedMachineFieldsPending = errors.New("machine field definitions are not ready")

// Steve separates CRD field definitions from the resource's permissions and
// routing schema. Older servers expose resourceFields inline instead.
func deployedMachineFields(provider string, call func(string, any) error) (map[string]deployedMachineField, error) {
	id := "rke-machine-config.cattle.io." + provider + "config"
	var schema struct {
		ResourceFields    map[string]deployedMachineField `json:"resourceFields"`
		CollectionMethods []string                        `json:"collectionMethods"`
	}
	if err := call("/v1/schemas/"+id, &schema); err != nil {
		return nil, err
	}
	fields := schema.ResourceFields
	creatable := false
	for _, method := range schema.CollectionMethods {
		creatable = creatable || method == "POST"
	}
	if schema.CollectionMethods != nil && !creatable {
		return nil, fmt.Errorf("this Rancher user cannot create machine configurations for %s; check its permissions", provider)
	}
	if len(fields) == 0 {
		if !creatable {
			return nil, fmt.Errorf("Runway does not recognize this Rancher's %s machine schema: neither inline fields nor collection permissions were provided. Continue in Rancher and report this schema format", provider)
		}
		var definitions struct {
			DefinitionType string `json:"definitionType"`
			Definitions    map[string]struct {
				ResourceFields map[string]deployedMachineField `json:"resourceFields"`
			} `json:"definitions"`
		}
		if err := call("/v1/schemaDefinitions/"+id, &definitions); err != nil {
			return nil, err
		}
		definition, exists := definitions.Definitions[definitions.DefinitionType]
		if definitions.DefinitionType == "" || !exists {
			return nil, fmt.Errorf("Runway does not recognize this Rancher's %s field-definition format: the root definition is missing. Continue in Rancher and report this schema format", provider)
		}
		fields = definition.ResourceFields
		for key, field := range fields {
			// Definition fields have no create flag; POST permission comes from
			// the resource schema. Preserve explicit read-only constraints.
			field.Create = !field.ReadOnly
			fields[key] = field
		}
	}
	result := map[string]deployedMachineField{}
	for key, field := range fields {
		if !field.Create || field.ReadOnly || deployedReservedField(key) {
			continue
		}
		// Cloud credentials are configured separately, never as machine fields.
		switch key {
		case "accessKey", "secretKey", "sessionToken", "token":
			continue
		}
		if strings.Contains(strings.ToLower(key), "password") || key == "rootPass" || key == "sshKeyContents" {
			field.Type = "password"
		}
		if provider == "linode" && (key == "region" || key == "instanceType" || key == "image") {
			field.Required = true
		}
		result[key] = field
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w for %s; check Node Drivers and retry loading options, or continue in Rancher", errDeployedMachineFieldsPending, provider)
	}
	return result, nil
}
