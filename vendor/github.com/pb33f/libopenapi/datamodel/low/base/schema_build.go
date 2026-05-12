// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// Build will perform a number of operations.
// Extraction of the following happens in this method:
//   - Extensions
//   - Type
//   - ExclusiveMinimum and ExclusiveMaximum
//   - Examples
//   - AdditionalProperties
//   - Discriminator
//   - ExternalDocs
//   - XML
//   - Properties
//   - AllOf, OneOf, AnyOf
//   - Not
//   - Items
//   - PrefixItems
//   - If
//   - Else
//   - Then
//   - DependentSchemas
//   - PatternProperties
//   - PropertyNames
//   - UnevaluatedItems
//   - UnevaluatedProperties
//   - Anchor
func (s *Schema) Build(ctx context.Context, root *yaml.Node, idx *index.SpecIndex) error {
	if root == nil {
		return fmt.Errorf("cannot build schema from a nil node")
	}
	root = utils.NodeAlias(root)
	utils.CheckForMergeNodes(root)

	s.reference = low.Reference{}
	s.Reference = &s.reference
	s.nodeStore = sync.Map{}
	s.Nodes = &s.nodeStore
	if root != nil && len(root.Content) > 0 {
		s.NodeMap.ExtractNodes(root, false)
	} else if root != nil {
		s.AddNode(root.Line, root)
	}
	s.Index = idx
	s.RootNode = root
	s.context = ctx
	s.index = idx

	isTransformed := false
	if s.ParentProxy != nil && s.ParentProxy.TransformedRef != nil {
		isTransformed = true
	}

	if !isTransformed {
		if h, _, _ := utils.IsNodeRefValue(root); h {
			ref, _, err, fctx := low.LocateRefNodeWithContext(ctx, root, idx)
			if ref != nil {
				root = ref
				if fctx != nil {
					ctx = fctx
				}
				if err != nil {
					if !idx.AllowCircularReferenceResolving() {
						return fmt.Errorf("build schema failed: %s", err.Error())
					}
				}
			} else {
				return fmt.Errorf("build schema failed: reference cannot be found: '%s', line %d, col %d",
					root.Content[1].Value, root.Content[1].Line, root.Content[1].Column)
			}
		}
	}

	if err := low.BuildModel(root, s); err != nil {
		return err
	}

	s.extractExtensions(root)

	if s.Required.Value != nil {
		for _, r := range s.Required.Value {
			s.AddNode(r.ValueNode.Line, r.ValueNode)
		}
	}

	if s.Enum.Value != nil {
		for _, e := range s.Enum.Value {
			s.AddNode(e.ValueNode.Line, e.ValueNode)
		}
	}

	_, typeLabel, typeValue := utils.FindKeyNodeFullTop(TypeLabel, root.Content)
	if typeValue != nil {
		if utils.IsNodeStringValue(typeValue) {
			s.Type = low.NodeReference[SchemaDynamicValue[string, []low.ValueReference[string]]]{
				KeyNode:   typeLabel,
				ValueNode: typeValue,
				Value:     SchemaDynamicValue[string, []low.ValueReference[string]]{N: 0, A: typeValue.Value},
			}
		}
		if utils.IsNodeArray(typeValue) {
			refs := make([]low.ValueReference[string], 0, len(typeValue.Content))
			for r := range typeValue.Content {
				refs = append(refs, low.ValueReference[string]{
					Value:     typeValue.Content[r].Value,
					ValueNode: typeValue.Content[r],
				})
			}
			s.Type = low.NodeReference[SchemaDynamicValue[string, []low.ValueReference[string]]]{
				KeyNode:   typeLabel,
				ValueNode: typeValue,
				Value:     SchemaDynamicValue[string, []low.ValueReference[string]]{N: 1, B: refs},
			}
		}
	}

	_, exMinLabel, exMinValue := utils.FindKeyNodeFullTop(ExclusiveMinimumLabel, root.Content)
	if exMinValue != nil {
		if idx != nil {
			if idx.GetConfig().SpecInfo.VersionNumeric >= 3.1 {
				val, _ := strconv.ParseFloat(exMinValue.Value, 64)
				s.ExclusiveMinimum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMinLabel,
					ValueNode: exMinValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 1, B: val},
				}
			}
			if idx.GetConfig().SpecInfo.VersionNumeric <= 3.0 {
				val, _ := strconv.ParseBool(exMinValue.Value)
				s.ExclusiveMinimum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMinLabel,
					ValueNode: exMinValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 0, A: val},
				}
			}
		} else {
			if utils.IsNodeBoolValue(exMinValue) {
				val, _ := strconv.ParseBool(exMinValue.Value)
				s.ExclusiveMinimum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMinLabel,
					ValueNode: exMinValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 0, A: val},
				}
			}
			if utils.IsNodeIntValue(exMinValue) {
				val, _ := strconv.ParseFloat(exMinValue.Value, 64)
				s.ExclusiveMinimum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMinLabel,
					ValueNode: exMinValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 1, B: val},
				}
			}
		}
	}

	_, exMaxLabel, exMaxValue := utils.FindKeyNodeFullTop(ExclusiveMaximumLabel, root.Content)
	if exMaxValue != nil {
		if idx != nil {
			if idx.GetConfig().SpecInfo.VersionNumeric >= 3.1 {
				val, _ := strconv.ParseFloat(exMaxValue.Value, 64)
				s.ExclusiveMaximum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMaxLabel,
					ValueNode: exMaxValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 1, B: val},
				}
			}
			if idx.GetConfig().SpecInfo.VersionNumeric <= 3.0 {
				val, _ := strconv.ParseBool(exMaxValue.Value)
				s.ExclusiveMaximum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMaxLabel,
					ValueNode: exMaxValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 0, A: val},
				}
			}
		} else {
			if utils.IsNodeBoolValue(exMaxValue) {
				val, _ := strconv.ParseBool(exMaxValue.Value)
				s.ExclusiveMaximum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMaxLabel,
					ValueNode: exMaxValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 0, A: val},
				}
			}
			if utils.IsNodeIntValue(exMaxValue) {
				val, _ := strconv.ParseFloat(exMaxValue.Value, 64)
				s.ExclusiveMaximum = low.NodeReference[*SchemaDynamicValue[bool, float64]]{
					KeyNode:   exMaxLabel,
					ValueNode: exMaxValue,
					Value:     &SchemaDynamicValue[bool, float64]{N: 1, B: val},
				}
			}
		}
	}

	_, schemaRefLabel, schemaRefNode := utils.FindKeyNodeFullTop(SchemaTypeLabel, root.Content)
	if schemaRefNode != nil {
		s.SchemaTypeRef = low.NodeReference[string]{
			Value: schemaRefNode.Value, KeyNode: schemaRefLabel, ValueNode: schemaRefNode,
		}
	}

	_, idLabel, idNode := utils.FindKeyNodeFullTop(IdLabel, root.Content)
	if idNode != nil {
		s.Id = low.NodeReference[string]{
			Value: idNode.Value, KeyNode: idLabel, ValueNode: idNode,
		}
	}

	_, anchorLabel, anchorNode := utils.FindKeyNodeFullTop(AnchorLabel, root.Content)
	if anchorNode != nil {
		s.Anchor = low.NodeReference[string]{
			Value: anchorNode.Value, KeyNode: anchorLabel, ValueNode: anchorNode,
		}
	}

	_, dynamicAnchorLabel, dynamicAnchorNode := utils.FindKeyNodeFullTop(DynamicAnchorLabel, root.Content)
	if dynamicAnchorNode != nil {
		s.DynamicAnchor = low.NodeReference[string]{
			Value: dynamicAnchorNode.Value, KeyNode: dynamicAnchorLabel, ValueNode: dynamicAnchorNode,
		}
	}

	_, dynamicRefLabel, dynamicRefNode := utils.FindKeyNodeFullTop(DynamicRefLabel, root.Content)
	if dynamicRefNode != nil {
		s.DynamicRef = low.NodeReference[string]{
			Value: dynamicRefNode.Value, KeyNode: dynamicRefLabel, ValueNode: dynamicRefNode,
		}
	}

	_, commentLabel, commentNode := utils.FindKeyNodeFullTop(CommentLabel, root.Content)
	if commentNode != nil {
		s.Comment = low.NodeReference[string]{
			Value: commentNode.Value, KeyNode: commentLabel, ValueNode: commentNode,
		}
	}

	_, vocabLabel, vocabNode := utils.FindKeyNodeFullTop(VocabularyLabel, root.Content)
	if vocabNode != nil && utils.IsNodeMap(vocabNode) {
		vocabularyMap := orderedmap.New[low.KeyReference[string], low.ValueReference[bool]]()
		var currentKey *yaml.Node
		for i, node := range vocabNode.Content {
			if i%2 == 0 {
				currentKey = node
				continue
			}
			boolVal, _ := strconv.ParseBool(node.Value)
			vocabularyMap.Set(low.KeyReference[string]{
				KeyNode: currentKey,
				Value:   currentKey.Value,
			}, low.ValueReference[bool]{
				Value:     boolVal,
				ValueNode: node,
			})
		}
		s.Vocabulary = low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[bool]]]{
			Value:     vocabularyMap,
			KeyNode:   vocabLabel,
			ValueNode: vocabNode,
		}
	}

	_, expLabel, expNode := utils.FindKeyNodeFullTop(ExampleLabel, root.Content)
	if expNode != nil {
		s.Example = low.NodeReference[*yaml.Node]{Value: expNode, KeyNode: expLabel, ValueNode: expNode}
		low.MergeRecursiveNodesIfLineAbsent(s.Nodes, expNode)
	}

	_, expArrLabel, expArrNode := utils.FindKeyNodeFullTop(ExamplesLabel, root.Content)
	if expArrNode != nil {
		if utils.IsNodeArray(expArrNode) {
			examples := make([]low.ValueReference[*yaml.Node], 0, len(expArrNode.Content))
			for i := range expArrNode.Content {
				examples = append(examples, low.ValueReference[*yaml.Node]{Value: expArrNode.Content[i], ValueNode: expArrNode.Content[i]})
			}
			s.Examples = low.NodeReference[[]low.ValueReference[*yaml.Node]]{
				Value:     examples,
				ValueNode: expArrNode,
				KeyNode:   expArrLabel,
			}
			low.MergeRecursiveNodesIfLineAbsent(s.Nodes, expArrNode)
		}
	}

	addPropsIsBool := false
	addPropsBoolValue := true
	_, addPLabel, addPValue := utils.FindKeyNodeFullTop(AdditionalPropertiesLabel, root.Content)
	if addPValue != nil {
		if utils.IsNodeBoolValue(addPValue) {
			addPropsIsBool = true
			addPropsBoolValue, _ = strconv.ParseBool(addPValue.Value)
		}
	}
	if addPropsIsBool {
		s.AdditionalProperties = low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]{
			Value: &SchemaDynamicValue[*SchemaProxy, bool]{
				B: addPropsBoolValue,
				N: 1,
			},
			KeyNode:   addPLabel,
			ValueNode: addPValue,
		}
	}

	_, discLabel, discNode := utils.FindKeyNodeFullTop(DiscriminatorLabel, root.Content)
	if discNode != nil {
		var discriminator Discriminator
		_ = low.BuildModel(discNode, &discriminator)
		discriminator.KeyNode = discLabel
		discriminator.RootNode = discNode
		discriminator.Nodes = low.ExtractNodes(ctx, discNode)
		s.Discriminator = low.NodeReference[*Discriminator]{Value: &discriminator, KeyNode: discLabel, ValueNode: discNode}
		low.AppendRecursiveNodes(&discriminator, discNode)
	}

	_, extDocLabel, extDocNode := utils.FindKeyNodeFullTop(ExternalDocsLabel, root.Content)
	if extDocNode != nil {
		var exDoc ExternalDoc
		_ = low.BuildModel(extDocNode, &exDoc)
		_ = exDoc.Build(ctx, extDocLabel, extDocNode, idx)
		exDoc.Nodes = low.ExtractNodes(ctx, extDocNode)
		s.ExternalDocs = low.NodeReference[*ExternalDoc]{Value: &exDoc, KeyNode: extDocLabel, ValueNode: extDocNode}
	}

	_, xmlLabel, xmlNode := utils.FindKeyNodeFullTop(XMLLabel, root.Content)
	if xmlNode != nil {
		var xml XML
		_ = low.BuildModel(xmlNode, &xml)
		_ = xml.Build(xmlNode, idx)
		xml.Nodes = low.ExtractNodes(ctx, xmlNode)
		s.XML = low.NodeReference[*XML]{Value: &xml, KeyNode: xmlLabel, ValueNode: xmlNode}
	}

	props, err := buildPropertyMap(ctx, s, root, idx, PropertiesLabel)
	if err != nil {
		return err
	}
	if props != nil {
		s.Properties = *props
	}

	props, err = buildPropertyMap(ctx, s, root, idx, DependentSchemasLabel)
	if err != nil {
		return err
	}
	if props != nil {
		s.DependentSchemas = *props
	}

	depReq, err := buildDependentRequiredMap(root, DependentRequiredLabel)
	if err != nil {
		return err
	}
	if depReq != nil {
		s.DependentRequired = *depReq
	}

	props, err = buildPropertyMap(ctx, s, root, idx, PatternPropertiesLabel)
	if err != nil {
		return err
	}
	if props != nil {
		s.PatternProperties = *props
	}

	itemsIsBool := false
	itemsBoolValue := false
	_, itemsLabel, itemsValue := utils.FindKeyNodeFullTop(ItemsLabel, root.Content)
	if itemsValue != nil {
		if utils.IsNodeBoolValue(itemsValue) {
			itemsIsBool = true
			itemsBoolValue, _ = strconv.ParseBool(itemsValue.Value)
		}
	}
	if itemsIsBool {
		s.Items = low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]{
			Value: &SchemaDynamicValue[*SchemaProxy, bool]{
				B: itemsBoolValue,
				N: 1,
			},
			KeyNode:   itemsLabel,
			ValueNode: itemsValue,
		}
	}

	unevalIsBool := false
	unevalBoolValue := true
	_, unevalLabel, unevalValue := utils.FindKeyNodeFullTop(UnevaluatedPropertiesLabel, root.Content)
	if unevalValue != nil {
		if utils.IsNodeBoolValue(unevalValue) {
			unevalIsBool = true
			unevalBoolValue, _ = strconv.ParseBool(unevalValue.Value)
		}
	}
	if unevalIsBool {
		s.UnevaluatedProperties = low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]{
			Value: &SchemaDynamicValue[*SchemaProxy, bool]{
				B: unevalBoolValue,
				N: 1,
			},
			KeyNode:   unevalLabel,
			ValueNode: unevalValue,
		}
	}

	var allOf, anyOf, oneOf, prefixItems []low.ValueReference[*SchemaProxy]
	var items, not, contains, sif, selse, sthen, propertyNames, unevalItems, unevalProperties, addProperties, contentSch low.ValueReference[*SchemaProxy]

	_, allOfLabel, allOfValue := utils.FindKeyNodeFullTop(AllOfLabel, root.Content)
	_, anyOfLabel, anyOfValue := utils.FindKeyNodeFullTop(AnyOfLabel, root.Content)
	_, oneOfLabel, oneOfValue := utils.FindKeyNodeFullTop(OneOfLabel, root.Content)
	_, notLabel, notValue := utils.FindKeyNodeFullTop(NotLabel, root.Content)
	_, prefixItemsLabel, prefixItemsValue := utils.FindKeyNodeFullTop(PrefixItemsLabel, root.Content)
	_, containsLabel, containsValue := utils.FindKeyNodeFullTop(ContainsLabel, root.Content)
	_, sifLabel, sifValue := utils.FindKeyNodeFullTop(IfLabel, root.Content)
	_, selseLabel, selseValue := utils.FindKeyNodeFullTop(ElseLabel, root.Content)
	_, sthenLabel, sthenValue := utils.FindKeyNodeFullTop(ThenLabel, root.Content)
	_, propNamesLabel, propNamesValue := utils.FindKeyNodeFullTop(PropertyNamesLabel, root.Content)
	_, unevalItemsLabel, unevalItemsValue := utils.FindKeyNodeFullTop(UnevaluatedItemsLabel, root.Content)
	unevalPropsLabel, unevalPropsValue := unevalLabel, unevalValue
	addPropsLabel, addPropsValue := addPLabel, addPValue
	_, contentSchLabel, contentSchValue := utils.FindKeyNodeFullTop(ContentSchemaLabel, root.Content)

	if ctx == nil {
		ctx = context.Background()
	}

	if err := assignBuiltSchemaList(ctx, allOfLabel, allOfValue, idx, &allOf); err != nil {
		return fmt.Errorf("failed to build schema: %w", err)
	}
	if err := assignBuiltSchemaList(ctx, anyOfLabel, anyOfValue, idx, &anyOf); err != nil {
		return fmt.Errorf("failed to build schema: %w", err)
	}
	if err := assignBuiltSchemaList(ctx, oneOfLabel, oneOfValue, idx, &oneOf); err != nil {
		return fmt.Errorf("failed to build schema: %w", err)
	}
	if err := assignBuiltSchemaList(ctx, prefixItemsLabel, prefixItemsValue, idx, &prefixItems); err != nil {
		return fmt.Errorf("failed to build schema: %w", err)
	}
	if err := assignBuiltSchema(ctx, notLabel, notValue, idx, &not); err != nil {
		return fmt.Errorf("failed to build schema: %w", err)
	}
	if err := assignBuiltSchema(ctx, containsLabel, containsValue, idx, &contains); err != nil {
		return fmt.Errorf("failed to build schema: %w", err)
	}
	if !itemsIsBool {
		if err := assignBuiltSchema(ctx, itemsLabel, itemsValue, idx, &items); err != nil {
			return fmt.Errorf("failed to build schema: %w", err)
		}
	}
	if err := assignBuiltSchema(ctx, sifLabel, sifValue, idx, &sif); err != nil {
		return fmt.Errorf("failed to build schema: %w", err)
	}
	if err := assignBuiltSchema(ctx, selseLabel, selseValue, idx, &selse); err != nil {
		return fmt.Errorf("failed to build schema: %w", err)
	}
	if err := assignBuiltSchema(ctx, sthenLabel, sthenValue, idx, &sthen); err != nil {
		return fmt.Errorf("failed to build schema: %w", err)
	}
	if err := assignBuiltSchema(ctx, propNamesLabel, propNamesValue, idx, &propertyNames); err != nil {
		return fmt.Errorf("failed to build schema: %w", err)
	}
	if err := assignBuiltSchema(ctx, unevalItemsLabel, unevalItemsValue, idx, &unevalItems); err != nil {
		return fmt.Errorf("failed to build schema: %w", err)
	}
	if !unevalIsBool {
		if err := assignBuiltSchema(ctx, unevalPropsLabel, unevalPropsValue, idx, &unevalProperties); err != nil {
			return fmt.Errorf("failed to build schema: %w", err)
		}
	}
	if !addPropsIsBool {
		if err := assignBuiltSchema(ctx, addPropsLabel, addPropsValue, idx, &addProperties); err != nil {
			return fmt.Errorf("failed to build schema: %w", err)
		}
	}
	if err := assignBuiltSchema(ctx, contentSchLabel, contentSchValue, idx, &contentSch); err != nil {
		return fmt.Errorf("failed to build schema: %w", err)
	}

	if len(anyOf) > 0 {
		s.AnyOf = low.NodeReference[[]low.ValueReference[*SchemaProxy]]{
			Value:     anyOf,
			KeyNode:   anyOfLabel,
			ValueNode: anyOfValue,
		}
	}
	if len(oneOf) > 0 {
		s.OneOf = low.NodeReference[[]low.ValueReference[*SchemaProxy]]{
			Value:     oneOf,
			KeyNode:   oneOfLabel,
			ValueNode: oneOfValue,
		}
	}
	if len(allOf) > 0 {
		s.AllOf = low.NodeReference[[]low.ValueReference[*SchemaProxy]]{
			Value:     allOf,
			KeyNode:   allOfLabel,
			ValueNode: allOfValue,
		}
	}
	if !not.IsEmpty() {
		s.Not = low.NodeReference[*SchemaProxy]{
			Value:     not.Value,
			KeyNode:   notLabel,
			ValueNode: notValue,
		}
	}
	if !itemsIsBool && !items.IsEmpty() {
		s.Items = low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]{
			Value: &SchemaDynamicValue[*SchemaProxy, bool]{
				A: items.Value,
			},
			KeyNode:   itemsLabel,
			ValueNode: itemsValue,
		}
	}
	if len(prefixItems) > 0 {
		s.PrefixItems = low.NodeReference[[]low.ValueReference[*SchemaProxy]]{
			Value:     prefixItems,
			KeyNode:   prefixItemsLabel,
			ValueNode: prefixItemsValue,
		}
	}
	if !contains.IsEmpty() {
		s.Contains = low.NodeReference[*SchemaProxy]{
			Value:     contains.Value,
			KeyNode:   containsLabel,
			ValueNode: containsValue,
		}
	}
	if !sif.IsEmpty() {
		s.If = low.NodeReference[*SchemaProxy]{
			Value:     sif.Value,
			KeyNode:   sifLabel,
			ValueNode: sifValue,
		}
	}
	if !selse.IsEmpty() {
		s.Else = low.NodeReference[*SchemaProxy]{
			Value:     selse.Value,
			KeyNode:   selseLabel,
			ValueNode: selseValue,
		}
	}
	if !sthen.IsEmpty() {
		s.Then = low.NodeReference[*SchemaProxy]{
			Value:     sthen.Value,
			KeyNode:   sthenLabel,
			ValueNode: sthenValue,
		}
	}
	if !propertyNames.IsEmpty() {
		s.PropertyNames = low.NodeReference[*SchemaProxy]{
			Value:     propertyNames.Value,
			KeyNode:   propNamesLabel,
			ValueNode: propNamesValue,
		}
	}
	if !unevalItems.IsEmpty() {
		s.UnevaluatedItems = low.NodeReference[*SchemaProxy]{
			Value:     unevalItems.Value,
			KeyNode:   unevalItemsLabel,
			ValueNode: unevalItemsValue,
		}
	}
	if !unevalIsBool && !unevalProperties.IsEmpty() {
		s.UnevaluatedProperties = low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]{
			Value: &SchemaDynamicValue[*SchemaProxy, bool]{
				A: unevalProperties.Value,
			},
			KeyNode:   unevalPropsLabel,
			ValueNode: unevalPropsValue,
		}
	}
	if !addPropsIsBool && !addProperties.IsEmpty() {
		s.AdditionalProperties = low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]{
			Value: &SchemaDynamicValue[*SchemaProxy, bool]{
				A: addProperties.Value,
			},
			KeyNode:   addPropsLabel,
			ValueNode: addPropsValue,
		}
	}
	if !contentSch.IsEmpty() {
		s.ContentSchema = low.NodeReference[*SchemaProxy]{
			Value:     contentSch.Value,
			KeyNode:   contentSchLabel,
			ValueNode: contentSchValue,
		}
	}
	return nil
}
