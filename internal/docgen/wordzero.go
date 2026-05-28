package docgen

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wnyr/openapi2word/internal/model"

	"github.com/ZeroHawkeye/wordZero/pkg/document"
	"github.com/ZeroHawkeye/wordZero/pkg/style"
)

// BuildDocx 使用 WordZero 将接口文档渲染为 .docx 字节。
func BuildDocx(doc model.APIDocument, meta model.Meta, selectedIDs map[string]bool) ([]byte, error) {
	d := document.New()

	// 统一将标题样式设为黑色 (000000)
	sm := d.GetStyleManager()
	for _, styleID := range []string{style.StyleHeading1, style.StyleHeading2, style.StyleHeading3} {
		if s := sm.GetStyle(styleID); s != nil {
			if s.RunPr == nil {
				s.RunPr = &style.RunProperties{}
			}
			s.RunPr.Color = &style.Color{Val: "000000"}
		}
	}

	title := firstNonEmpty(meta.Title, doc.Info.Title, "API 文档")
	titlePara := d.AddParagraph(title)
	titlePara.SetStyle(style.StyleHeading1)
	titlePara.SetAlignment(document.AlignCenter)

	d.AddParagraph("文档修订记录").SetStyle(style.StyleHeading2)
	addRevisionTable(d, meta)

	tagGroups := groupEndpointsByTag(doc.Endpoints, selectedIDs)
	tagIndex := 1
	for _, tg := range tagGroups {
		d.AddParagraph(fmt.Sprintf("%s、 %s", cnIndex(tagIndex), tg.Tag)).SetStyle(style.StyleHeading1)
		tagIndex++

		sectionIndex := 1
		for _, e := range tg.Endpoints {
			fullURL := e.Path
			name := firstNonEmpty(e.Summary, e.OperationID, e.Path)
			d.AddParagraph(fmt.Sprintf("%d. %s", sectionIndex, name)).SetStyle(style.StyleHeading2)

			d.AddParagraph(fmt.Sprintf("%d.1 接口说明", sectionIndex)).SetStyle(style.StyleHeading3)
			desc := firstNonEmpty(e.Description, e.Summary, "无")
			d.AddParagraph(desc)

			d.AddParagraph(fmt.Sprintf("%d.2 接口设计", sectionIndex)).SetStyle(style.StyleHeading3)
			if err := addInterfaceTable(d, e, fullURL); err != nil {
				return nil, err
			}
			sectionIndex++
		}
	}

	return d.ToBytes()
}

// addRevisionTable 写入修订记录表。
func addRevisionTable(d *document.Document, meta model.Meta) {
	revs := meta.Revisions
	if len(revs) == 0 {
		revs = []model.Revision{{
			Version:     meta.Version,
			Summary:     "",
			ChangeDate:  "",
			ChangeOwner: meta.Author,
		}}
	}

	table := d.AddTable(&document.TableConfig{
		Rows:  len(revs) + 1,
		Cols:  4,
		Width: 8000,
	})
	setCell(table, 0, 0, "版本号")
	setCell(table, 0, 1, "简要说明")
	setCell(table, 0, 2, "变更日期")
	setCell(table, 0, 3, "变更人")

	for i, r := range revs {
		row := i + 1
		setCell(table, row, 0, r.Version)
		setCell(table, row, 1, r.Summary)
		setCell(table, row, 2, r.ChangeDate)
		setCell(table, row, 3, r.ChangeOwner)
	}
}

// addInterfaceTable 写入单个接口的详情表格。
func addInterfaceTable(d *document.Document, e model.Endpoint, fullURL string) error {
	reqGroups := groupRequestFields(e.Request)
	reqFields := flattenGroups(reqGroups)
	reqBrief := joinFieldNames(reqFields)
	respBrief := joinFieldNames(e.Response)
	if reqBrief == "" {
		reqBrief = "无"
	}
	if respBrief == "" {
		respBrief = "无"
	}

	reqRows := countParamRows(reqFields)
	respRows := countResponseRows(e.Response)
	baseRows := 4                  // 地址、方式、请求参数、响应参数
	rows := baseRows + 2 + reqRows // 请求参数说明 + 表头
	if respRows > 0 {
		rows += 2 + respRows // 响应参数说明 + 表头
	}

	table := d.AddTable(&document.TableConfig{
		Rows:  rows,
		Cols:  5,
		Width: 8000,
	})

	r := 0
	setCell(table, r, 0, "接口地址", true)
	setCell(table, r, 1, fullURL)
	mergeRow(table, r, 1, 4)
	r++
	setCell(table, r, 0, "请求方式", true)
	setCell(table, r, 1, e.Method)
	mergeRow(table, r, 1, 4)
	r++
	setCell(table, r, 0, "请求参数示例", true)
	setCodeCell(table, r, 1, buildRequestExamples(reqGroups))
	mergeRow(table, r, 1, 4)

	r++
	setCell(table, r, 0, "响应参数示例", true)
	setCodeCell(table, r, 1, buildExampleJSON(e.Response))
	mergeRow(table, r, 1, 4)
	r++

	setCell(table, r, 0, "请求参数说明", true)
	setCell(table, r, 1, "")
	mergeRow(table, r, 1, 4)
	r++
	writeParamHeader(table, r, true)
	r++
	writeParamRows(table, &r, reqFields, true)

	if respRows > 0 {
		setCell(table, r, 0, "响应参数说明", true)
		setCell(table, r, 1, "")
		mergeRow(table, r, 1, 4)
		r++
		writeParamHeader(table, r, false)
		r++
		writeParamRows(table, &r, e.Response, false)
	}

	return nil
}

// writeParamHeader 写入字段表头行。
func writeParamHeader(t *document.Table, row int, isRequest ...bool) {
	if len(isRequest) > 0 && isRequest[0] {
		setCell(t, row, 0, "字段名称", true)
		setCell(t, row, 1, "字段类型", true)
		setCell(t, row, 2, "参数位置", true)
		setCell(t, row, 3, "是否必传", true)
		setCell(t, row, 4, "备注", true)
	} else {
		setCell(t, row, 0, "字段名称", true)
		setCell(t, row, 1, "字段类型", true)
		setCell(t, row, 2, "备注", true)
		mergeRow(t, row, 2, 4)
	}
}

// writeFieldRows 递归写入字段（用于请求参数，父子以缩进展示）。
func writeFieldRows(t *document.Table, row *int, fields []model.Field, level int, showRequired bool) {
	for _, f := range fields {
		indent := strings.Repeat("  ", level)
		name := indent + f.Name
		if level > 0 {
			name = indent + "└ " + f.Name
		}
		setCell(t, *row, 0, name)
		setCell(t, *row, 1, f.Type)
		if showRequired {
			loc := f.Location
			if loc == "" {
				loc = "body"
			}
			_ = t.SetCellFormattedText(*row, 2, loc, &document.TextFormat{
				FontColor: "2563eb",
				Bold:      true,
			})
			reqVal := "否"
			if f.Required {
				reqVal = "是"
			}
			setCell(t, *row, 3, reqVal)
			setCell(t, *row, 4, f.Description)
		} else {
			setCell(t, *row, 2, f.Description)
			mergeRow(t, *row, 2, 4)
		}
		*row++

		if len(f.Children) > 0 {
			writeFieldRows(t, row, f.Children, level+1, showRequired)
		}
	}
}

// writeParamRows 按“父级先、子级后”的规则写入参数。
// 规则：
// 1. 先写顶层字段（不展开子集）。
// 2. 对每个包含子集的字段，写“xxx说明”标题行 + 表头 + 该字段的直接子集。
// 3. 子集中若还有子集，继续按同样规则展开。
func writeParamRows(t *document.Table, row *int, fields []model.Field, showRequired bool) {
	seen := map[string]bool{}
	writeTopLevelRows(t, row, fields, showRequired)

	for _, f := range fields {
		if len(f.Children) == 0 {
			continue
		}
		writeParamSection(t, row, f, seen, showRequired)
	}
}

// writeParamSection 写入某个字段的说明段（标题 + 表头 + 直接子集 + 子集递归）。
func writeParamSection(t *document.Table, row *int, field model.Field, seen map[string]bool, showRequired bool) {
	key := responseSectionKey(field)
	if key != "" && seen[key] {
		return
	}
	if key != "" {
		seen[key] = true
	}

	title := responseSectionTitle(field)
	setCell(t, *row, 0, title, true)
	setCell(t, *row, 1, "")
	mergeRow(t, *row, 1, 4)
	*row++

	writeParamHeader(t, *row, showRequired)
	*row++

	// 仅写直接子集，保持层级清晰。
	writeTopLevelRows(t, row, field.Children, showRequired)

	// 继续处理子集中仍含子集的字段
	for _, child := range field.Children {
		if len(child.Children) == 0 {
			continue
		}
		writeParamSection(t, row, child, seen, showRequired)
	}
}

// writeTopLevelRows 写入字段列表，不展开子集。
func writeTopLevelRows(t *document.Table, row *int, fields []model.Field, showRequired bool) {
	for _, f := range fields {
		setCell(t, *row, 0, f.Name)
		setCell(t, *row, 1, f.Type)
		if showRequired {
			loc := f.Location
			if loc == "" {
				loc = "body"
			}
			_ = t.SetCellFormattedText(*row, 2, loc, &document.TextFormat{
				FontColor: "2563eb",
				Bold:      true,
			})
			reqVal := "否"
			if f.Required {
				reqVal = "是"
			}
			setCell(t, *row, 3, reqVal)
			setCell(t, *row, 4, f.Description)
		} else {
			setCell(t, *row, 2, f.Description)
			mergeRow(t, *row, 2, 4)
		}
		*row++
	}
}

// countResponseRows 统计响应参数展示所需行数（父级先、子级后）。
func countResponseRows(fields []model.Field) int {
	seen := map[string]bool{}
	total := len(fields)
	total += countResponseSections(fields, seen)
	return total
}

// countResponseSections 仅统计“说明段”的行数，不重复计算顶层字段。
func countResponseSections(fields []model.Field, seen map[string]bool) int {
	total := 0
	for _, f := range fields {
		if len(f.Children) == 0 {
			continue
		}
		key := responseSectionKey(f)
		if key != "" && seen[key] {
			continue
		}
		if key != "" {
			seen[key] = true
		}
		total += 2 + len(f.Children) // 标题 + 表头 + 直接子集
		total += countResponseSections(f.Children, seen)
	}
	return total
}

// responseSectionTitle 生成“xxx说明”的标题文本。
func responseSectionTitle(field model.Field) string {
	if inner, ok := arrayInnerType(field.Type); ok && inner != "" {
		return inner + "说明"
	}
	if isComplexType(field.Type) {
		return field.Type + "说明"
	}
	return field.Name + "说明"
}

func responseSectionKey(field model.Field) string {
	if inner, ok := arrayInnerType(field.Type); ok && inner != "" {
		return inner
	}
	if isComplexType(field.Type) {
		return field.Type
	}
	if field.Name != "" {
		return field.Name
	}
	return ""
}

func arrayInnerType(typ string) (string, bool) {
	if strings.HasPrefix(typ, "array<") && strings.HasSuffix(typ, ">") {
		return strings.TrimSuffix(strings.TrimPrefix(typ, "array<"), ">"), true
	}
	return "", false
}

func isComplexType(typ string) bool {
	if typ == "" {
		return false
	}
	primitive := map[string]bool{
		"string":  true,
		"integer": true,
		"number":  true,
		"boolean": true,
		"object":  true,
		"any":     true,
	}
	if primitive[typ] {
		return false
	}
	if strings.Contains(typ, "|") {
		return false
	}
	if strings.Contains(typ, "(") {
		return false
	}
	return true
}

// buildRequestExamples 生成请求参数示例（按 path/query/header/cookie/body/formData）。
func buildRequestExamples(groups []requestGroup) string {
	lines := []string{}
	for _, g := range groups {
		switch g.Location {
		case "path":
			if ex := buildPathExample(g.Fields); ex != "" {
				lines = append(lines, ex)
			}
		case "query":
			if ex := buildQueryExample(g.Fields); ex != "" {
				lines = append(lines, ex)
			}
		case "header":
			if ex := buildHeaderExample(g.Fields); ex != "" {
				lines = append(lines, ex)
			}
		case "cookie":
			if ex := buildCookieExample(g.Fields); ex != "" {
				lines = append(lines, ex)
			}
		case "body":
			if ex := buildBodyExample(g.Fields); ex != "" {
				lines = append(lines, ex)
			}
		case "formData":
			if ex := buildFormExample(g.Fields); ex != "" {
				lines = append(lines, ex)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func buildPathExample(fields []model.Field) string {
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.Name != "" {
			parts = append(parts, "{"+f.Name+"}")
		}
	}
	return "/" + strings.Join(parts, "/")
}

func buildQueryExample(fields []model.Field) string {
	if len(fields) == 0 {
		return ""
	}
	qs := make([]string, 0, len(fields))
	for _, f := range fields {
		qs = append(qs, f.Name+"="+exampleScalar(f))
	}
	return "?" + strings.Join(qs, "&")
}

func buildHeaderExample(fields []model.Field) string {
	if len(fields) == 0 {
		return ""
	}
	f := fields[0]
	name := f.Name
	if name == "" {
		name = "X-Api-Key"
	}
	return name + ": " + exampleScalar(f)
}

func buildCookieExample(fields []model.Field) string {
	if len(fields) == 0 {
		return ""
	}
	f := fields[0]
	name := f.Name
	if name == "" {
		name = "sessionId"
	}
	return name + "=" + exampleScalar(f)
}

func buildBodyExample(fields []model.Field) string {
	if len(fields) == 0 {
		return ""
	}
	// 若只有一个 body 字段，直接输出该字段示例，避免额外包一层 {"body": ...}
	if len(fields) == 1 && strings.EqualFold(fields[0].Name, "body") {
		val := exampleValue(fields[0], 0, map[string]bool{})
		b, err := json.MarshalIndent(val, "", "  ")
		if err != nil {
			return "{}"
		}
		return string(b)
	}
	return buildExampleJSON(fields)
}

func buildFormExample(fields []model.Field) string {
	if len(fields) == 0 {
		return ""
	}
	return buildQueryExample(fields)
}

func exampleScalar(field model.Field) string {
	if strings.HasPrefix(field.Type, "integer") || strings.HasPrefix(field.Type, "number") {
		return "123"
	}
	if strings.HasPrefix(field.Type, "boolean") {
		return "true"
	}
	if strings.HasPrefix(field.Type, "array") {
		return "item"
	}
	return "string"
}

type requestGroup struct {
	Location string
	Fields   []model.Field
}

// groupRequestFields 按参数位置分组并排序（path/query/header/cookie/body/formData）。
func groupRequestFields(fields []model.Field) []requestGroup {
	order := []string{"path", "query", "header", "cookie", "body", "formData"}
	group := map[string][]model.Field{}
	var unknown []model.Field
	for _, f := range fields {
		if f.Location == "" {
			unknown = append(unknown, f)
			continue
		}
		group[f.Location] = append(group[f.Location], f)
	}
	out := make([]requestGroup, 0, len(order)+1)
	for _, key := range order {
		if len(group[key]) > 0 {
			out = append(out, requestGroup{Location: key, Fields: group[key]})
		}
	}
	if len(unknown) > 0 {
		out = append(out, requestGroup{Location: "other", Fields: unknown})
	}
	return out
}

func flattenGroups(groups []requestGroup) []model.Field {
	out := []model.Field{}
	for _, g := range groups {
		out = append(out, g.Fields...)
	}
	return out
}

// writeRequestRowsByLocation 按位置分组写入请求参数。
func writeRequestRowsByLocation(t *document.Table, row *int, groups []requestGroup) {
	for _, g := range groups {
		writeFieldRows(t, row, g.Fields, 0, true)
	}
}

// countParamRows 统计参数展示行数（与 writeParamRows 的规则一致）。
func countParamRows(fields []model.Field) int {
	seen := map[string]bool{}
	total := len(fields)
	total += countParamSections(fields, seen)
	return total
}

// countParamSections 统计“说明段”的行数，不重复计算顶层字段。
func countParamSections(fields []model.Field, seen map[string]bool) int {
	total := 0
	for _, f := range fields {
		if len(f.Children) == 0 {
			continue
		}
		key := responseSectionKey(f)
		if key != "" && seen[key] {
			continue
		}
		if key != "" {
			seen[key] = true
		}
		total += 2 + len(f.Children) // 标题 + 表头 + 直接子集
		total += countParamSections(f.Children, seen)
	}
	return total
}

// buildExampleJSON 基于字段结构生成示例 JSON。
func buildExampleJSON(fields []model.Field) string {
	obj := map[string]interface{}{}
	for _, f := range fields {
		obj[f.Name] = exampleValue(f, 0, map[string]bool{})
	}
	b, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

func exampleValue(field model.Field, depth int, seen map[string]bool) interface{} {
	if depth > 6 {
		return "..."
	}
	key := responseSectionKey(field)
	if key != "" && seen[key] {
		return "..."
	}
	if key != "" {
		seen[key] = true
		defer delete(seen, key)
	}

	if strings.HasPrefix(field.Type, "array<") {
		item := model.Field{
			Name:     "item",
			Type:     strings.TrimSuffix(strings.TrimPrefix(field.Type, "array<"), ">"),
			Children: field.Children,
		}
		return []interface{}{exampleValue(item, depth+1, seen)}
	}
	if field.Type == "object" || len(field.Children) > 0 {
		obj := map[string]interface{}{}
		for _, c := range field.Children {
			obj[c.Name] = exampleValue(c, depth+1, seen)
		}
		return obj
	}
	if strings.HasPrefix(field.Type, "integer") || strings.HasPrefix(field.Type, "number") {
		return 0
	}
	if strings.HasPrefix(field.Type, "boolean") {
		return false
	}
	return "string"
}

type tagGroup struct {
	Tag       string
	Endpoints []model.Endpoint
}

func groupEndpointsByTag(endpoints []model.Endpoint, selectedIDs map[string]bool) []tagGroup {
	order := []string{}
	group := map[string][]model.Endpoint{}
	for _, e := range endpoints {
		if len(selectedIDs) > 0 && !selectedIDs[e.ID] {
			continue
		}
		tag := e.Tag
		if tag == "" {
			tag = "未分组"
		}
		if _, ok := group[tag]; !ok {
			order = append(order, tag)
		}
		group[tag] = append(group[tag], e)
	}
	out := make([]tagGroup, 0, len(order))
	for _, tag := range order {
		out = append(out, tagGroup{Tag: tag, Endpoints: group[tag]})
	}
	return out
}

func cnIndex(i int) string {
	digits := []string{"一", "二", "三", "四", "五", "六", "七", "八", "九", "十"}
	if i >= 1 && i <= 10 {
		return digits[i-1]
	}
	if i < 20 {
		return "十" + digits[i-11]
	}
	if i%10 == 0 {
		return digits[i/10-1] + "十"
	}
	return digits[i/10-1] + "十" + digits[i%10-1]
}

// countFieldRows 统计递归字段所需行数（父级+子级）。
func countFieldRows(fields []model.Field) int {
	total := 0
	for _, f := range fields {
		total++
		if len(f.Children) > 0 {
			total += countFieldRows(f.Children)
		}
	}
	return total
}

// setCell 写入单元格文本，可选置灰背景。
func setCell(t *document.Table, row, col int, text string, shade ...bool) {
	_ = t.SetCellText(row, col, text)
	if len(shade) > 0 && shade[0] {
		_ = t.SetCellShading(row, col, &document.ShadingConfig{
			Pattern:         document.ShadingPatternSolid,
			BackgroundColor: "E6E6E6",
		})
	}
}

// setCodeCell 写入代码块风格（等宽字体 + 浅灰背景）。
func setCodeCell(t *document.Table, row, col int, text string) {
	_ = t.SetCellText(row, col, text)
	_ = t.SetCellFormat(row, col, &document.CellFormat{
		TextFormat: &document.TextFormat{
			FontFamily: "Courier New",
			FontSize:   10,
		},
		BackgroundColor: "F5F5F5",
	})
}

// mergeRow 横向合并一行中的单元格。
func mergeRow(t *document.Table, row, startCol, endCol int) {
	_ = t.MergeCellsHorizontal(row, startCol, endCol)
}

// joinFieldNames 生成顶层字段的简要摘要（逗号分隔）。
func joinFieldNames(fields []model.Field) string {
	if len(fields) == 0 {
		return ""
	}
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.Name != "" {
			names = append(names, f.Name)
		}
	}
	return strings.Join(names, ", ")
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
