package test

import (
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
)

func writePrivateConfigAtomically(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".tool-config-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func setStringSequenceValue(mapping *yaml.Node, key string, values []string) {
	sequenceNode := mappingValue(mapping, key)
	if sequenceNode == nil {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{},
		)
		sequenceNode = mapping.Content[len(mapping.Content)-1]
	}

	sequenceNode.Kind = yaml.SequenceNode
	sequenceNode.Tag = "!!seq"
	sequenceNode.Style = 0
	sequenceNode.Content = make([]*yaml.Node, 0, len(values))
	for _, value := range values {
		sequenceNode.Content = append(sequenceNode.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Style: yaml.DoubleQuotedStyle,
			Value: value,
		})
	}
}

func setStringLiteralSequenceValue(mapping *yaml.Node, key string, values []string) {
	sequenceNode := mappingValue(mapping, key)
	if sequenceNode == nil {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{},
		)
		sequenceNode = mapping.Content[len(mapping.Content)-1]
	}

	sequenceNode.Kind = yaml.SequenceNode
	sequenceNode.Tag = "!!seq"
	sequenceNode.Style = 0
	sequenceNode.Content = make([]*yaml.Node, 0, len(values))
	for _, value := range values {
		sequenceNode.Content = append(sequenceNode.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Style: yaml.LiteralStyle,
			Value: strings.TrimSpace(value),
		})
	}
}

func setStringMapValue(mapping *yaml.Node, key string, keys []string, values []string) {
	mapNode := mappingValue(mapping, key)
	if mapNode == nil {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{},
		)
		mapNode = mapping.Content[len(mapping.Content)-1]
	}

	mapNode.Kind = yaml.MappingNode
	mapNode.Tag = "!!map"
	mapNode.Style = 0
	mapNode.Content = nil
	seen := map[string]bool{}
	for i, keyValue := range keys {
		if seen[keyValue] {
			continue
		}
		seen[keyValue] = true
		mapNode.Content = append(mapNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: keyValue},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: values[i]},
		)
	}
}

func setLinodeDownstreamPlanSequenceValue(mapping *yaml.Node, key string, plans []settings.LinodeDownstreamPlan) {
	sequenceNode := mappingValue(mapping, key)
	if sequenceNode == nil {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{},
		)
		sequenceNode = mapping.Content[len(mapping.Content)-1]
	}

	sequenceNode.Kind = yaml.SequenceNode
	sequenceNode.Tag = "!!seq"
	sequenceNode.Style = 0
	sequenceNode.Content = make([]*yaml.Node, 0, len(plans))
	for _, plan := range plans {
		planNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setBoolValue(planNode, "enabled", plan.Enabled)
		setStringValue(planNode, "distribution", plan.Distribution)
		if plan.KubernetesVersion != "" {
			setStringValue(planNode, "kubernetes_version", plan.KubernetesVersion)
		}
		setStringValue(planNode, "region", plan.Region)
		setStringValue(planNode, "instance_type", plan.InstanceType)
		setStringValue(planNode, "image", plan.Image)
		sequenceNode.Content = append(sequenceNode.Content, planNode)
	}
}

func setStringValue(mapping *yaml.Node, key string, value string) {
	valueNode := mappingValue(mapping, key)
	if valueNode == nil {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{},
		)
		valueNode = mapping.Content[len(mapping.Content)-1]
	}

	valueNode.Kind = yaml.ScalarNode
	valueNode.Tag = "!!str"
	valueNode.Style = yaml.DoubleQuotedStyle
	valueNode.Value = value
}

func setBoolValue(mapping *yaml.Node, key string, value bool) {
	valueNode := mappingValue(mapping, key)
	if valueNode == nil {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{},
		)
		valueNode = mapping.Content[len(mapping.Content)-1]
	}

	valueNode.Kind = yaml.ScalarNode
	valueNode.Tag = "!!bool"
	valueNode.Style = 0
	if value {
		valueNode.Value = "true"
	} else {
		valueNode.Value = "false"
	}
}

func setIntValue(mapping *yaml.Node, key string, value int) {
	valueNode := mappingValue(mapping, key)
	if valueNode == nil {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{},
		)
		valueNode = mapping.Content[len(mapping.Content)-1]
	}

	valueNode.Kind = yaml.ScalarNode
	valueNode.Tag = "!!int"
	valueNode.Style = 0
	valueNode.Value = fmt.Sprintf("%d", value)
}
