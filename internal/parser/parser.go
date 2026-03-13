package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/wnyr/openapi2word/internal/model"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

// ParseDocument 解析 Swagger/OpenAPI（v2/v3）JSON/YAML 到内部模型。
func ParseDocument(data []byte) (*model.APIDocument, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("empty input")
	}

	data, err := normalizeJSONorYAML(data)
	if err != nil {
		return nil, err
	}

	// 识别 Swagger v2（顶层没有 openapi 字段）
	var v2Check struct {
		Swagger string `json:"swagger"`
	}
	if json.Unmarshal(data, &v2Check) == nil && v2Check.Swagger != "" {
		var v2 openapi2.T
		if err := json.Unmarshal(data, &v2); err != nil {
			return nil, fmt.Errorf("invalid swagger v2: %w", err)
		}
		doc, err := openapi2conv.ToV3(&v2)
		if err != nil {
			return nil, fmt.Errorf("failed to convert swagger v2 to v3: %w", err)
		}
		return buildModel(doc)
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("failed to load openapi v3: %w", err)
	}

	if err := loader.ResolveRefsIn(doc, nil); err != nil {
		return nil, err
	}

	return buildModel(doc)
}

// normalizeJSONorYAML 如果是 YAML 则转换为 JSON 字节。
func normalizeJSONorYAML(data []byte) ([]byte, error) {
	var js interface{}
	if json.Unmarshal(data, &js) == nil {
		return data, nil
	}
	var ys interface{}
	if err := yaml.Unmarshal(data, &ys); err != nil {
		return nil, fmt.Errorf("invalid json/yaml: %w", err)
	}
	return json.Marshal(ys)
}

// buildModel 将已解析的 OpenAPI v3 文档映射为 APIDocument。
func buildModel(doc *openapi3.T) (*model.APIDocument, error) {
	out := &model.APIDocument{}
	if doc.Info != nil {
		out.Info = model.Info{
			Title:       doc.Info.Title,
			Version:     doc.Info.Version,
			Description: doc.Info.Description,
		}
	}
	for _, s := range doc.Servers {
		if s != nil && s.URL != "" {
			out.Servers = append(out.Servers, s.URL)
		}
	}
	for _, t := range doc.Tags {
		if t != nil {
			out.Tags = append(out.Tags, model.Tag{Name: t.Name, Description: t.Description})
		}
	}

	pathMap := doc.Paths.Map()
	for _, path := range sortedPathKeys(pathMap) {
		item := pathMap[path]
		if item == nil {
			continue
		}
		ops := item.Operations()
		for _, method := range sortedOpKeys(ops) {
			op := ops[method]
			if op == nil {
				continue
			}
			e := model.Endpoint{
				ID:          buildEndpointID(method, path, op.OperationID),
				Path:        path,
				Method:      strings.ToUpper(method),
				Summary:     op.Summary,
				Description: op.Description,
				OperationID: op.OperationID,
			}
			if len(op.Tags) > 0 {
				e.Tag = op.Tags[0]
			}

			// 收集 query/path/header 参数
			params := collectParameters(item, op)
			e.Request = append(e.Request, params...)

			// 请求体 schema（优先 application/json）
			bodySchema := extractRequestBodySchema(op)
			if bodySchema != nil {
				fields := schemaToFields("body", bodySchema, true)
				setLocationRecursive(fields, "body")
				e.Request = append(e.Request, fields...)
				e.ReqEntities = append(e.ReqEntities, entitiesFromSchema("RequestBody", bodySchema)...)
			}
			if len(params) > 0 {
				// 参数实体放在请求列表最前
				e.ReqEntities = append([]model.Entity{{Name: "RequestParams", Fields: params}}, e.ReqEntities...)
			}

			// 响应 schema（优先 2xx）
			respSchema := extractResponseSchema(op)
			if respSchema != nil {
				fields := schemaToFields("response", respSchema, false)
				e.Response = append(e.Response, fields...)
				e.RespEntities = append(e.RespEntities, entitiesFromSchema("Response", respSchema)...)
			}

			out.Endpoints = append(out.Endpoints, e)
		}
	}

	return out, nil
}

// buildEndpointID 生成稳定的接口标识。
func buildEndpointID(method, path, opID string) string {
	if opID != "" {
		return opID
	}
	return strings.ToLower(method) + " " + path
}

// collectParameters 合并路径级与方法级参数。
func collectParameters(item *openapi3.PathItem, op *openapi3.Operation) []model.Field {
	var params openapi3.Parameters
	params = append(params, item.Parameters...)
	params = append(params, op.Parameters...)

	out := make([]model.Field, 0, len(params))
	for _, p := range params {
		if p == nil || p.Value == nil {
			continue
		}

		var field model.Field
		if p.Value.Schema != nil {
			field = schemaToField(p.Value.Name, p.Value.Schema, p.Value.Required)
		} else if p.Value.Content != nil {
			for _, mt := range p.Value.Content {
				if mt != nil && mt.Schema != nil {
					field = schemaToField(p.Value.Name, mt.Schema, p.Value.Required)
					break
				}
			}
		}

		// 兼容 v2 参数类型解析
		if field.Type == "any" || field.Type == "" {
			var extractedType string

			// 1. 参数级扩展字段
			if t, ok := p.Value.Extensions["type"].(string); ok {
				extractedType = t
			} else if t, ok := p.Value.Extensions["x-type"].(string); ok {
				extractedType = t
			}

			// 2. schema 级扩展字段
			if extractedType == "" && p.Value.Schema != nil && p.Value.Schema.Value != nil {
				if t, ok := p.Value.Schema.Value.Extensions["type"].(string); ok {
					extractedType = t
				}
			}

			if extractedType != "" {
				// 同时尝试解析 format
				format, _ := p.Value.Extensions["format"].(string)
				if format == "" && p.Value.Schema != nil && p.Value.Schema.Value != nil {
					format = p.Value.Schema.Value.Format
				}
				if format != "" {
					extractedType = extractedType + "(" + format + ")"
				}

				// 处理 v2 数组 items
				if extractedType == "array" {
					if items, ok := p.Value.Extensions["items"].(map[string]interface{}); ok {
						if it, ok := items["type"].(string); ok {
							extractedType = "array<" + it + ">"
						}
					}
				}

				field.Type = extractedType
			}
		}

		// 确保 Name/Required/Location 来自参数对象
		if field.Name == "" {
			field.Name = p.Value.Name
		}
		if !field.Required && p.Value.Required {
			field.Required = true
		}
		if field.Location == "" {
			field.Location = p.Value.In
		}
		if field.Type == "" {
			field.Type = "any"
		}
		if field.Description == "" {
			field.Description = p.Value.Description
		}

		out = append(out, field)
	}
	return out
}

// extractRequestBodySchema 获取请求体 schema。
func extractRequestBodySchema(op *openapi3.Operation) *openapi3.SchemaRef {
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return nil
	}
	content := op.RequestBody.Value.Content
	if content == nil {
		return nil
	}
	if mt := content.Get("application/json"); mt != nil {
		return mt.Schema
	}
	for _, mt := range content {
		if mt != nil {
			return mt.Schema
		}
	}
	return nil
}

// extractResponseSchema 选择可能的成功响应 schema（2xx/default）。
func extractResponseSchema(op *openapi3.Operation) *openapi3.SchemaRef {
	if op.Responses == nil {
		return nil
	}
	candidates := []string{"200", "201", "202", "204"}
	for _, code := range candidates {
		if r := op.Responses.Value(code); r != nil {
			return responseMediaSchema(r)
		}
	}
	for code, r := range op.Responses.Map() {
		if strings.HasPrefix(code, "2") {
			return responseMediaSchema(r)
		}
	}
	if r := op.Responses.Value("default"); r != nil {
		return responseMediaSchema(r)
	}
	return nil
}

// responseMediaSchema 返回第一个可用的媒体类型 schema。
func responseMediaSchema(r *openapi3.ResponseRef) *openapi3.SchemaRef {
	if r == nil || r.Value == nil || r.Value.Content == nil {
		return nil
	}
	if mt := r.Value.Content.Get("application/json"); mt != nil {
		return mt.Schema
	}
	for _, mt := range r.Value.Content {
		if mt != nil {
			return mt.Schema
		}
	}
	return nil
}

// schemaToFields 将 schema 展平成用于摘要展示的字段列表。
func schemaToFields(rootName string, ref *openapi3.SchemaRef, required bool) []model.Field {
	if ref == nil {
		return nil
	}
	field := schemaToField(rootName, ref, required)
	if rootName == "body" || rootName == "response" {
		// 不展示 body/response 本身，直接从第一层字段开始
		if len(field.Children) > 0 {
			return field.Children
		}
	}
	if field.Type == "object" && len(field.Children) > 0 {
		return field.Children
	}
	return []model.Field{field}
}

// schemaToField 将 schema 转为 Field，并保留子级字段。
func schemaToField(name string, ref *openapi3.SchemaRef, required bool) model.Field {
	return schemaToFieldWithSeen(name, ref, required, map[*openapi3.Schema]bool{})
}

func schemaToFieldWithSeen(name string, ref *openapi3.SchemaRef, required bool, seen map[*openapi3.Schema]bool) model.Field {
	field := model.Field{Name: name, Required: required, Type: "any"}
	if ref == nil || ref.Value == nil {
		return field
	}
	s := ref.Value
	field.Description = s.Description

	if seen[s] {
		// 避免循环引用导致无限递归，尽量返回可读类型名
		if field.Type == "any" || field.Type == "array<any>" {
			field.Type = bestTypeName(ref)
		}
		return field
	}

	typeName := ""
	if s.Type != nil && len(s.Type.Slice()) > 0 {
		typeName = strings.Join(s.Type.Slice(), "|")
	}

	// OpenAPI 3 中可能省略 type（如 $ref）
	if typeName == "" {
		if len(s.Properties) > 0 {
			typeName = "object"
		} else if s.Items != nil {
			typeName = "array"
		} else if ref.Ref != "" {
			// It's a reference, we'll handle it below
			typeName = "object"
		} else {
			typeName = "any"
		}
	}

	// 尝试用 Ref 名称作为复杂类型名
	if ref.Ref != "" {
		parts := strings.Split(ref.Ref, "/")
		refName := parts[len(parts)-1]
		if typeName == "object" || typeName == "any" {
			typeName = refName
		}
	}

	// 合并 format（例如 integer(int64)）
	if s.Format != "" && (typeName == "integer" || typeName == "string" || typeName == "number") {
		typeName = typeName + "(" + s.Format + ")"
	}

	field.Type = typeName

	seen[s] = true
	defer delete(seen, s)

	if (s.Type != nil && s.Type.Is("array")) || s.Items != nil {
		itemType := "any"
		var itemChildren []model.Field
		if s.Items != nil {
			if seen[s.Items.Value] {
				itemType = bestTypeName(s.Items)
			} else {
				item := schemaToFieldWithSeen("item", s.Items, false, seen)
				itemType = item.Type
				itemChildren = item.Children
			}
		}
		field.Type = "array<" + itemType + ">"
		if itemChildren != nil {
			field.Children = itemChildren
		}
		return field
	}

	if (s.Type != nil && s.Type.Is("object")) || len(s.Properties) > 0 {
		req := map[string]bool{}
		for _, r := range s.Required {
			req[r] = true
		}
		for _, pname := range sortedSchemaKeys(s.Properties) {
			pref := s.Properties[pname]
			child := schemaToFieldWithSeen(pname, pref, req[pname], seen)
			field.Children = append(field.Children, child)
		}
		return field
	}

	return field
}

func bestTypeName(ref *openapi3.SchemaRef) string {
	if ref == nil {
		return "any"
	}
	if ref.Ref != "" {
		parts := strings.Split(ref.Ref, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	if ref.Value != nil {
		if ref.Value.Title != "" {
			return ref.Value.Title
		}
	}
	return "any"
}

// entitiesFromSchema 生成父级优先的实体列表，用于文档表格。
func entitiesFromSchema(rootName string, ref *openapi3.SchemaRef) []model.Entity {
	root := schemaToField(rootName, ref, true)
	out := []model.Entity{}
	collectEntitiesPreorder(&out, rootName, root)
	return out
}

// collectEntitiesPreorder 先加入父级实体，再加入子级实体。
func collectEntitiesPreorder(out *[]model.Entity, name string, field model.Field) {
	if field.Type == "object" && len(field.Children) > 0 {
		*out = append(*out, model.Entity{Name: name, Fields: field.Children})
		for _, child := range field.Children {
			if child.Type == "object" && len(child.Children) > 0 {
				collectEntitiesPreorder(out, name+"."+child.Name, child)
			}
			if strings.HasPrefix(child.Type, "array<") && len(child.Children) > 0 {
				collectEntitiesPreorder(out, name+"."+child.Name+"[]", model.Field{
					Name:     child.Name,
					Type:     "object",
					Children: child.Children,
				})
			}
		}
	}
}

// setLocationRecursive 为字段及其子字段统一设置位置标记。
func setLocationRecursive(fields []model.Field, location string) {
	for i := range fields {
		if fields[i].Location == "" {
			fields[i].Location = location
		}
		if len(fields[i].Children) > 0 {
			setLocationRecursive(fields[i].Children, location)
		}
	}
}

// sortedPathKeys 返回稳定的路径顺序。
func sortedPathKeys(m map[string]*openapi3.PathItem) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedOpKeys 返回稳定的 HTTP 方法顺序。
func sortedOpKeys(m map[string]*openapi3.Operation) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedSchemaKeys 返回稳定的字段顺序。
func sortedSchemaKeys(m openapi3.Schemas) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
