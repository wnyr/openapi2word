package model

type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Field struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Required    bool    `json:"required"`
	Description string  `json:"description"`
	Location    string  `json:"in,omitempty"`
	Children    []Field `json:"children,omitempty"`
}

type Entity struct {
	Name   string  `json:"name"`
	Fields []Field `json:"fields"`
}

type Endpoint struct {
	ID           string   `json:"id"`
	Tag          string   `json:"tag"`
	Path         string   `json:"path"`
	Method       string   `json:"method"`
	Summary      string   `json:"summary"`
	Description  string   `json:"description"`
	OperationID  string   `json:"operation_id"`
	Request      []Field  `json:"request"`
	Response     []Field  `json:"response"`
	ReqEntities  []Entity `json:"request_entities"`
	RespEntities []Entity `json:"response_entities"`
}

type APIDocument struct {
	Info      Info       `json:"info"`
	Servers   []string   `json:"servers"`
	Tags      []Tag      `json:"tags"`
	Endpoints []Endpoint `json:"endpoints"`
}

type Revision struct {
	Version     string `json:"version"`
	Summary     string `json:"summary"`
	ChangeDate  string `json:"change_date"`
	ChangeOwner string `json:"change_owner"`
}

type Meta struct {
	Title     string     `json:"title"`
	Author    string     `json:"author"`
	Version   string     `json:"version"`
	Revisions []Revision `json:"revisions"`
}

type GenerateRequest struct {
	Doc         APIDocument `json:"doc"`
	Meta        Meta        `json:"meta"`
	EndpointIDs []string    `json:"endpoint_ids"`
}
