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
		normalizeV2Refs(&v2)
		normalizeV2MissingDefinitions(&v2)
		normalizeV2BodyParameters(&v2)
		// 优先走 v2 直接解析，避免 v2->v3 转换造成字段丢失
		if out, err := buildModelFromV2(&v2); err == nil {
			return out, nil
		}
		// 回退到 v2->v3 转换
		doc, err := openapi2conv.ToV3(&v2)
		if err != nil {
			return nil, err
		}
		return buildModel(doc)
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("failed to load openapi v3: %w", err)
	}

	normalizeV3Refs(doc)

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

// normalizeV3Refs 将 v3 中不存在的泛型/非法 schema 引用替换为已有的基础定义。
// 例如 RestResponse«Void» -> RestResponse（当基础定义存在时）。
func normalizeV3Refs(doc *openapi3.T) {
	if doc == nil || doc.Components.Schemas == nil {
		return
	}
	names := map[string]bool{}
	for k := range doc.Components.Schemas {
		names[k] = true
	}

	replaceRef := func(ref *openapi3.SchemaRef) {
		if ref == nil || ref.Ref == "" {
			return
		}
		parts := strings.Split(ref.Ref, "/")
		if len(parts) == 0 {
			return
		}
		name := parts[len(parts)-1]
		if names[name] {
			return
		}
		base := baseDefName(name)
		if base != name && names[base] {
			parts[len(parts)-1] = base
			ref.Ref = strings.Join(parts, "/")
		}
	}

	var walkSchema func(s *openapi3.Schema)
	walkSchema = func(s *openapi3.Schema) {
		if s == nil {
			return
		}
		if s.Not != nil {
			replaceRef(s.Not)
			if s.Not.Value != nil {
				walkSchema(s.Not.Value)
			}
		}
		for _, r := range s.AllOf {
			replaceRef(r)
			if r != nil && r.Value != nil {
				walkSchema(r.Value)
			}
		}
		for _, r := range s.AnyOf {
			replaceRef(r)
			if r != nil && r.Value != nil {
				walkSchema(r.Value)
			}
		}
		for _, r := range s.OneOf {
			replaceRef(r)
			if r != nil && r.Value != nil {
				walkSchema(r.Value)
			}
		}
		if s.Items != nil {
			replaceRef(s.Items)
			if s.Items.Value != nil {
				walkSchema(s.Items.Value)
			}
		}
		for _, r := range s.Properties {
			replaceRef(r)
			if r != nil && r.Value != nil {
				walkSchema(r.Value)
			}
		}
		if r := s.AdditionalProperties.Schema; r != nil {
			replaceRef(r)
			if r.Value != nil {
				walkSchema(r.Value)
			}
		}
	}

	// components schemas
	for _, r := range doc.Components.Schemas {
		replaceRef(r)
		if r != nil && r.Value != nil {
			walkSchema(r.Value)
		}
	}

	// components parameters
	for _, p := range doc.Components.Parameters {
		if p == nil || p.Value == nil {
			continue
		}
		replaceRef(p.Value.Schema)
		if p.Value.Schema != nil && p.Value.Schema.Value != nil {
			walkSchema(p.Value.Schema.Value)
		}
		for _, mt := range p.Value.Content {
			if mt == nil || mt.Schema == nil {
				continue
			}
			replaceRef(mt.Schema)
			if mt.Schema.Value != nil {
				walkSchema(mt.Schema.Value)
			}
		}
	}

	// components requestBodies/responses/headers
	for _, rb := range doc.Components.RequestBodies {
		if rb == nil || rb.Value == nil {
			continue
		}
		for _, mt := range rb.Value.Content {
			if mt == nil || mt.Schema == nil {
				continue
			}
			replaceRef(mt.Schema)
			if mt.Schema.Value != nil {
				walkSchema(mt.Schema.Value)
			}
		}
	}
	for _, resp := range doc.Components.Responses {
		if resp == nil || resp.Value == nil {
			continue
		}
		for _, mt := range resp.Value.Content {
			if mt == nil || mt.Schema == nil {
				continue
			}
			replaceRef(mt.Schema)
			if mt.Schema.Value != nil {
				walkSchema(mt.Schema.Value)
			}
		}
	}
	for _, h := range doc.Components.Headers {
		if h == nil || h.Value == nil {
			continue
		}
		replaceRef(h.Value.Schema)
		if h.Value.Schema != nil && h.Value.Schema.Value != nil {
			walkSchema(h.Value.Schema.Value)
		}
	}

	// paths
	for _, item := range doc.Paths.Map() {
		if item == nil {
			continue
		}
		for _, p := range item.Parameters {
			if p == nil || p.Value == nil {
				continue
			}
			replaceRef(p.Value.Schema)
			if p.Value.Schema != nil && p.Value.Schema.Value != nil {
				walkSchema(p.Value.Schema.Value)
			}
			for _, mt := range p.Value.Content {
				if mt == nil || mt.Schema == nil {
					continue
				}
				replaceRef(mt.Schema)
				if mt.Schema.Value != nil {
					walkSchema(mt.Schema.Value)
				}
			}
		}
		for _, op := range item.Operations() {
			if op == nil {
				continue
			}
			for _, p := range op.Parameters {
				if p == nil || p.Value == nil {
					continue
				}
				replaceRef(p.Value.Schema)
				if p.Value.Schema != nil && p.Value.Schema.Value != nil {
					walkSchema(p.Value.Schema.Value)
				}
				for _, mt := range p.Value.Content {
					if mt == nil || mt.Schema == nil {
						continue
					}
					replaceRef(mt.Schema)
					if mt.Schema.Value != nil {
						walkSchema(mt.Schema.Value)
					}
				}
			}
			if op.RequestBody != nil && op.RequestBody.Value != nil {
				for _, mt := range op.RequestBody.Value.Content {
					if mt == nil || mt.Schema == nil {
						continue
					}
					replaceRef(mt.Schema)
					if mt.Schema.Value != nil {
						walkSchema(mt.Schema.Value)
					}
				}
			}
			if op.Responses != nil {
				for _, r := range op.Responses.Map() {
					if r == nil || r.Value == nil {
						continue
					}
					for _, mt := range r.Value.Content {
						if mt == nil || mt.Schema == nil {
							continue
						}
						replaceRef(mt.Schema)
						if mt.Schema.Value != nil {
							walkSchema(mt.Schema.Value)
						}
					}
				}
			}
		}
	}
}

// normalizeV2Refs 处理 v2 definitions 中不合法的 ref 名称（如包含“/”）。
// 将 definitions key 和所有 $ref 同步替换为安全名称，避免 v2->v3 解析失败。
func normalizeV2Refs(doc *openapi2.T) {
	if doc == nil {
		return
	}
	rename := map[string]string{}
	if doc.Definitions != nil {
		for k := range doc.Definitions {
			n := sanitizeDefName(k)
			if n != k {
				rename[k] = n
			}
		}
		if len(rename) > 0 {
			newDefs := map[string]*openapi2.SchemaRef{}
			for k, v := range doc.Definitions {
				if nk, ok := rename[k]; ok {
					newDefs[nk] = v
				} else {
					newDefs[k] = v
				}
			}
			doc.Definitions = newDefs
		}
	}

	defNames := map[string]bool{}
	for k := range doc.Definitions {
		defNames[k] = true
	}

	replaceRef := func(ref *openapi2.SchemaRef) {
		if ref == nil || ref.Ref == "" {
			return
		}
		if strings.HasPrefix(ref.Ref, "#/definitions/") {
			name := strings.TrimPrefix(ref.Ref, "#/definitions/")
			if nk, ok := rename[name]; ok {
				ref.Ref = "#/definitions/" + nk
				return
			}
			nn := sanitizeDefName(name)
			if nn != name {
				ref.Ref = "#/definitions/" + nn
				name = nn
			}
			if !defNames[name] {
				base := baseDefName(name)
				if base != name && defNames[base] {
					ref.Ref = "#/definitions/" + base
				}
			}
		}
	}

	visitSchema := func(s *openapi2.Schema) {}
	var walkSchema func(s *openapi2.Schema)
	walkSchema = func(s *openapi2.Schema) {
		if s == nil {
			return
		}
		if s.Not != nil {
			replaceRef(s.Not)
			if s.Not.Value != nil {
				walkSchema(s.Not.Value)
			}
		}
		for _, r := range s.AllOf {
			replaceRef(r)
			if r != nil && r.Value != nil {
				walkSchema(r.Value)
			}
		}
		if s.Items != nil {
			replaceRef(s.Items)
			if s.Items.Value != nil {
				walkSchema(s.Items.Value)
			}
		}
		for _, r := range s.Properties {
			replaceRef(r)
			if r != nil && r.Value != nil {
				walkSchema(r.Value)
			}
		}
		_ = visitSchema
	}

	// walk definitions
	for _, r := range doc.Definitions {
		replaceRef(r)
		if r != nil && r.Value != nil {
			walkSchema(r.Value)
		}
	}
	// walk paths/operations parameters and responses
	for _, item := range doc.Paths {
		if item == nil {
			continue
		}
		for _, p := range item.Parameters {
			if p == nil {
				continue
			}
			replaceRef(p.Schema)
			replaceRef(p.Items)
			if p.Schema != nil && p.Schema.Value != nil {
				walkSchema(p.Schema.Value)
			}
		}
		for _, op := range item.Operations() {
			if op == nil {
				continue
			}
			for _, p := range op.Parameters {
				if p == nil {
					continue
				}
				replaceRef(p.Schema)
				replaceRef(p.Items)
				if p.Schema != nil && p.Schema.Value != nil {
					walkSchema(p.Schema.Value)
				}
			}
			for _, r := range op.Responses {
				if r == nil {
					continue
				}
				replaceRef(r.Schema)
				if r.Schema != nil && r.Schema.Value != nil {
					walkSchema(r.Schema.Value)
				}
			}
		}
	}
	// walk global parameters/responses
	for _, p := range doc.Parameters {
		if p == nil {
			continue
		}
		replaceRef(p.Schema)
		replaceRef(p.Items)
		if p.Schema != nil && p.Schema.Value != nil {
			walkSchema(p.Schema.Value)
		}
	}
	for _, r := range doc.Responses {
		if r == nil {
			continue
		}
		replaceRef(r.Schema)
		if r.Schema != nil && r.Schema.Value != nil {
			walkSchema(r.Schema.Value)
		}
	}
}

func sanitizeDefName(s string) string {
	if s == "" {
		return s
	}
	r := strings.NewReplacer("/", "_", "~", "_")
	return r.Replace(s)
}

func baseDefName(s string) string {
	if s == "" {
		return s
	}
	if i := strings.Index(s, "«"); i >= 0 {
		return s[:i]
	}
	if i := strings.Index(s, "<"); i >= 0 {
		return s[:i]
	}
	return s
}

// normalizeV2MissingDefinitions 为缺失的 definitions 补一个最小 schema（避免转换时找不到 ref）。
func normalizeV2MissingDefinitions(doc *openapi2.T) {
	if doc == nil {
		return
	}
	if doc.Definitions == nil {
		doc.Definitions = map[string]*openapi2.SchemaRef{}
	}

	refs := map[string]bool{}
	addRef := func(ref *openapi2.SchemaRef) {
		if ref == nil || ref.Ref == "" {
			return
		}
		if strings.HasPrefix(ref.Ref, "#/definitions/") {
			name := strings.TrimPrefix(ref.Ref, "#/definitions/")
			name = sanitizeDefName(name)
			base := baseDefName(name)
			if base != name {
				if _, ok := doc.Definitions[base]; ok {
					return
				}
			}
			refs[name] = true
		}
	}

	var walkSchema func(s *openapi2.Schema)
	walkSchema = func(s *openapi2.Schema) {
		if s == nil {
			return
		}
		if s.Not != nil {
			addRef(s.Not)
			if s.Not.Value != nil {
				walkSchema(s.Not.Value)
			}
		}
		for _, r := range s.AllOf {
			addRef(r)
			if r != nil && r.Value != nil {
				walkSchema(r.Value)
			}
		}
		if s.Items != nil {
			addRef(s.Items)
			if s.Items.Value != nil {
				walkSchema(s.Items.Value)
			}
		}
		for _, r := range s.Properties {
			addRef(r)
			if r != nil && r.Value != nil {
				walkSchema(r.Value)
			}
		}
	}

	// definitions
	for _, r := range doc.Definitions {
		addRef(r)
		if r != nil && r.Value != nil {
			walkSchema(r.Value)
		}
	}
	// paths
	for _, item := range doc.Paths {
		if item == nil {
			continue
		}
		for _, p := range item.Parameters {
			if p == nil {
				continue
			}
			addRef(p.Schema)
			addRef(p.Items)
			if p.Schema != nil && p.Schema.Value != nil {
				walkSchema(p.Schema.Value)
			}
		}
		for _, op := range item.Operations() {
			if op == nil {
				continue
			}
			for _, p := range op.Parameters {
				if p == nil {
					continue
				}
				addRef(p.Schema)
				addRef(p.Items)
				if p.Schema != nil && p.Schema.Value != nil {
					walkSchema(p.Schema.Value)
				}
			}
			for _, r := range op.Responses {
				if r == nil {
					continue
				}
				addRef(r.Schema)
				if r.Schema != nil && r.Schema.Value != nil {
					walkSchema(r.Schema.Value)
				}
			}
		}
	}
	// global parameters/responses
	for _, p := range doc.Parameters {
		if p == nil {
			continue
		}
		addRef(p.Schema)
		addRef(p.Items)
		if p.Schema != nil && p.Schema.Value != nil {
			walkSchema(p.Schema.Value)
		}
	}
	for _, r := range doc.Responses {
		if r == nil {
			continue
		}
		addRef(r.Schema)
		if r.Schema != nil && r.Schema.Value != nil {
			walkSchema(r.Schema.Value)
		}
	}

	for name := range refs {
		if _, ok := doc.Definitions[name]; ok {
			continue
		}
		var schema *openapi2.Schema
		if strings.EqualFold(name, "List") {
			schema = &openapi2.Schema{
				Type:  &openapi3.Types{"array"},
				Items: &openapi2.SchemaRef{Value: &openapi2.Schema{}},
			}
		} else {
			schema = &openapi2.Schema{Type: &openapi3.Types{"object"}}
		}
		doc.Definitions[name] = &openapi2.SchemaRef{Value: schema}
	}
}

// normalizeV2BodyParameters 合并 v2 多个 body 参数为单一对象，避免转换失败。
func normalizeV2BodyParameters(doc *openapi2.T) {
	if doc == nil || doc.Paths == nil {
		return
	}
	for _, item := range doc.Paths {
		if item == nil {
			continue
		}
		// 先移除 path 级 body 参数，交给 operation 合并
		item.Parameters, _ = splitBodyParams(item.Parameters)

		for _, op := range item.Operations() {
			if op == nil {
				continue
			}
			nonBody, bodyParams := splitBodyParams(op.Parameters)
			if len(bodyParams) <= 1 {
				op.Parameters = append(nonBody, bodyParams...)
				continue
			}

			merged := mergeBodyParams(bodyParams)
			op.Parameters = append(nonBody, merged)
		}
	}
}

func splitBodyParams(params openapi2.Parameters) (openapi2.Parameters, openapi2.Parameters) {
	var nonBody openapi2.Parameters
	var body openapi2.Parameters
	for _, p := range params {
		if p == nil {
			continue
		}
		if strings.EqualFold(p.In, "body") {
			body = append(body, p)
		} else {
			nonBody = append(nonBody, p)
		}
	}
	return nonBody, body
}

func mergeBodyParams(params openapi2.Parameters) *openapi2.Parameter {
	props := openapi2.Schemas{}
	required := []string{}
	anyRequired := false

	for i, p := range params {
		if p == nil {
			continue
		}
		name := p.Name
		if name == "" {
			name = fmt.Sprintf("body%d", i+1)
		}
		if p.Required {
			required = append(required, name)
			anyRequired = true
		}

		var ref *openapi2.SchemaRef
		if p.Schema != nil {
			ref = p.Schema
		} else {
			ref = &openapi2.SchemaRef{Value: &openapi2.Schema{
				Type:   p.Type,
				Format: p.Format,
				Items:  p.Items,
			}}
		}
		props[name] = ref
	}

	schema := &openapi2.Schema{
		Type:       &openapi3.Types{"object"},
		Properties: props,
	}
	if anyRequired {
		schema.Required = required
	}

	return &openapi2.Parameter{
		In:          "body",
		Name:        "body",
		Description: "Merged body parameters",
		Required:    anyRequired,
		Schema:      &openapi2.SchemaRef{Value: schema},
	}
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

// buildModelFromV2 直接从 Swagger v2 构建 APIDocument，避免 v2->v3 转换失败。
func buildModelFromV2(doc *openapi2.T) (*model.APIDocument, error) {
	out := &model.APIDocument{}
	out.Info = model.Info{
		Title:       doc.Info.Title,
		Version:     doc.Info.Version,
		Description: doc.Info.Description,
	}
	if doc.Host != "" {
		scheme := "http"
		if len(doc.Schemes) > 0 {
			scheme = doc.Schemes[0]
		}
		out.Servers = append(out.Servers, scheme+"://"+doc.Host+doc.BasePath)
	}
	for _, t := range doc.Tags {
		if t != nil {
			out.Tags = append(out.Tags, model.Tag{Name: t.Name, Description: t.Description})
		}
	}

	for _, path := range sortedV2PathKeys(doc.Paths) {
		item := doc.Paths[path]
		if item == nil {
			continue
		}
		ops := item.Operations()
		for _, method := range sortedV2OpKeys(ops) {
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

			// 合并参数
			params := append(openapi2.Parameters{}, item.Parameters...)
			params = append(params, op.Parameters...)

			// 处理非 body 参数
			for _, p := range params {
				if p == nil {
					continue
				}
				if strings.EqualFold(p.In, "body") {
					continue
				}
				f := v2ParamToField(p, doc.Definitions)
				e.Request = append(e.Request, f)
			}

			// 处理 body 参数（合并后只会有一个）
			for _, p := range params {
				if p == nil || !strings.EqualFold(p.In, "body") || p.Schema == nil {
					continue
				}
				fields := v2SchemaToFields("body", p.Schema, p.Required, doc.Definitions)
				setLocationRecursive(fields, "body")
				e.Request = append(e.Request, fields...)
				break
			}

			// 处理响应
			if resp := pickV2Response(op.Responses); resp != nil && resp.Schema != nil {
				fields := v2SchemaToFields("response", resp.Schema, false, doc.Definitions)
				e.Response = append(e.Response, fields...)
			}

			out.Endpoints = append(out.Endpoints, e)
		}
	}
	return out, nil
}

func pickV2Response(responses map[string]*openapi2.Response) *openapi2.Response {
	if responses == nil {
		return nil
	}
	for _, code := range []string{"200", "201", "202", "204"} {
		if r, ok := responses[code]; ok {
			return r
		}
	}
	for code, r := range responses {
		if strings.HasPrefix(code, "2") {
			return r
		}
	}
	if r, ok := responses["default"]; ok {
		return r
	}
	return nil
}

func v2ParamToField(p *openapi2.Parameter, defs map[string]*openapi2.SchemaRef) model.Field {
	if p.Schema != nil {
		f := v2SchemaToField(p.Name, p.Schema, p.Required, defs, map[string]bool{})
		f.Description = p.Description
		f.Location = p.In
		return f
	}
	field := model.Field{
		Name:        p.Name,
		Required:    p.Required,
		Description: p.Description,
		Location:    p.In,
		Type:        "any",
	}
	if p.Type != nil && len(p.Type.Slice()) > 0 {
		field.Type = strings.Join(p.Type.Slice(), "|")
	}
	if field.Type == "array" && p.Items != nil {
		item := v2SchemaToField("item", p.Items, false, defs, map[string]bool{})
		field.Type = "array<" + item.Type + ">"
	}
	if p.Format != "" && (field.Type == "integer" || field.Type == "string" || field.Type == "number") {
		field.Type = field.Type + "(" + p.Format + ")"
	}
	return field
}

func v2SchemaToFields(rootName string, ref *openapi2.SchemaRef, required bool, defs map[string]*openapi2.SchemaRef) []model.Field {
	field := v2SchemaToField(rootName, ref, required, defs, map[string]bool{})
	if rootName == "body" || rootName == "response" {
		if len(field.Children) > 0 {
			return field.Children
		}
		// 兜底：若响应引用了泛型占位定义（如 RestResponse«Void»），且自身无字段，
		// 尝试回退到基础定义（如 RestResponse）获取完整字段列表。
		if ref != nil && ref.Ref != "" {
			defName := strings.TrimPrefix(ref.Ref, "#/definitions/")
			defName = sanitizeDefName(defName)
			base := baseDefName(defName)
			if base != defName {
				if def, ok := defs[base]; ok && def != nil {
					baseField := v2SchemaToField(rootName, def, required, defs, map[string]bool{})
					if len(baseField.Children) > 0 {
						return baseField.Children
					}
				}
			}
		}
	}
	if field.Type == "object" && len(field.Children) > 0 {
		return field.Children
	}
	return []model.Field{field}
}

func v2SchemaToField(name string, ref *openapi2.SchemaRef, required bool, defs map[string]*openapi2.SchemaRef, seen map[string]bool) model.Field {
	field := model.Field{Name: name, Required: required, Type: "any"}
	if ref == nil {
		return field
	}
	if ref.Ref != "" {
		defName := strings.TrimPrefix(ref.Ref, "#/definitions/")
		defName = sanitizeDefName(defName)
		if seen[defName] {
			field.Type = defName
			return field
		}
		seen[defName] = true
		defer delete(seen, defName)
		if def, ok := defs[defName]; ok && def != nil {
			field = v2SchemaToField(name, def, required, defs, seen)
			base := baseDefName(defName)
			if base != defName {
				if baseDef, ok := defs[base]; ok && baseDef != nil {
					baseField := v2SchemaToField(name, baseDef, required, defs, seen)
					field = mergeFieldChildren(field, baseField)
				}
			}
			if field.Type == "object" || field.Type == "any" {
				field.Type = defName
			}
			return field
		}
		base := baseDefName(defName)
		if base != defName {
			if def, ok := defs[base]; ok && def != nil {
				field = v2SchemaToField(name, def, required, defs, seen)
				field.Type = base
				return field
			}
		}
		field.Type = defName
		return field
	}
	if ref.Value == nil {
		return field
	}
	s := ref.Value
	field.Description = s.Description

	typeName := ""
	if s.Type != nil && len(s.Type.Slice()) > 0 {
		typeName = strings.Join(s.Type.Slice(), "|")
	}
	if typeName == "" {
		if len(s.Properties) > 0 {
			typeName = "object"
		} else if s.Items != nil {
			typeName = "array"
		} else {
			typeName = "any"
		}
	}
	if s.Format != "" && (typeName == "integer" || typeName == "string" || typeName == "number") {
		typeName = typeName + "(" + s.Format + ")"
	}
	field.Type = typeName

	if typeName == "array" && s.Items != nil {
		item := v2SchemaToField("item", s.Items, false, defs, seen)
		field.Type = "array<" + item.Type + ">"
		field.Children = item.Children
		return field
	}
	if typeName == "object" && len(s.Properties) > 0 {
		req := map[string]bool{}
		for _, r := range s.Required {
			req[r] = true
		}
		for _, pname := range sortedV2SchemaKeys(s.Properties) {
			pref := s.Properties[pname]
			child := v2SchemaToField(pname, pref, req[pname], defs, seen)
			field.Children = append(field.Children, child)
		}
		return field
	}
	return field
}

// mergeFieldChildren 合并字段的子项（以 primary 为主，补齐 fallback 中缺失的子项）。
// 用于处理 v2 泛型占位 definitions（如 RestResponse«Void»）缺少公共字段的情况。
func mergeFieldChildren(primary, fallback model.Field) model.Field {
	if len(fallback.Children) == 0 {
		return primary
	}
	if len(primary.Children) == 0 {
		if primary.Type == "" || primary.Type == "any" {
			primary.Type = fallback.Type
		}
		if primary.Description == "" {
			primary.Description = fallback.Description
		}
		primary.Children = fallback.Children
		return primary
	}
	index := map[string]bool{}
	for _, c := range primary.Children {
		index[c.Name] = true
	}
	for _, c := range fallback.Children {
		if !index[c.Name] {
			primary.Children = append(primary.Children, c)
		}
	}
	return primary
}

func sortedV2PathKeys(m map[string]*openapi2.PathItem) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedV2SchemaKeys(m openapi2.Schemas) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedV2OpKeys(m map[string]*openapi2.Operation) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
