package uir

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ID struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	SourceCode `json:"sourceCode,omitempty"`
}

type ASTFunction struct {
	nodeBase
	FunctionType FunctionType `json:"functionType,omitempty"`
	Input        ASTRecord    `json:"input,omitempty"`
	Output       ASTRecord    `json:"output,omitempty"`
	Examples     []string     `json:"examples,omitempty"`
	// Errors represents a list of errors that can be returned by an endpoint
	Errors []ASTError `json:"errors,omitempty"`
	// Parameters represents the input parameters for the function
}

type EndpointPointMethod string

const (
	EndpointMethodGet     EndpointPointMethod = "GET"
	EndpointMethodPost    EndpointPointMethod = "POST"
	EndpointMethodPut     EndpointPointMethod = "PUT"
	EndpointMethodDelete  EndpointPointMethod = "DELETE"
	EndpointMethodPatch   EndpointPointMethod = "PATCH"
	EndpointMethodOptions EndpointPointMethod = "OPTIONS"
	EndpointMethodHead    EndpointPointMethod = "HEAD"
)

type ASTEndpoint struct {
	nodeBase
	// HTTP Method e.g. "GET", "POST" or "PUT"
	Method       EndpointPointMethod `json:"method,omitempty"`
	EndpointType EndpointType        `json:"endpointType,omitempty"`
	Input        ASTRecord           `json:"input,omitempty"`
	Output       ASTRecord           `json:"output,omitempty"`
	Examples     []string            `json:"examples,omitempty"`
	// Errors represents a list of errors that can be returned by an endpoint
	Errors []ASTError `json:"errors,omitempty"`
}

func (e ASTEndpoint) GetBaseUrl() string {
	return e.GetIdentifier().Module
}

func (e ASTEndpoint) GetPath() string {
	parts := []string{}
	if e.GetBaseUrl() != "" {
		parts = append(parts, e.GetBaseUrl())
	}
	if e.Package != "" {
		parts = append(parts, e.Package)
	}
	if e.Type != "" {
		parts = append(parts, e.Type)
	}
	if e.Method != "" {
		parts = append(parts, string(e.Method))
	}
	return strings.Join(parts, "/")

}

// PersistentBodyMixin implementation for ASTEndpoint
func (e ASTEndpoint) GetPersistentBody() json.RawMessage {
	if len(e.Errors) == 0 {
		return nil
	}
	data, err := json.Marshal(e.Errors)
	if err != nil {
		return nil
	}
	return data
}

func (e *ASTEndpoint) LoadPersistentBody(data json.RawMessage) error {
	if len(data) == 0 {
		e.Errors = nil
		return nil
	}
	var errors []ASTError
	if err := json.Unmarshal(data, &errors); err != nil {
		return fmt.Errorf("failed to unmarshal ASTEndpoint errors: %w", err)
	}
	e.Errors = errors
	return nil
}

type ASTError struct {
	ID          `json:",inline"`
	ErrorType   ASTErrorType `json:"errorType,omitempty"`
	SourceCode  `json:"sourceCode,omitempty"`
	Description string `json:"description,omitempty"`
	// Numeric Code e.g. HTTP status code or other protocol specific code
	Code TypedValue `json:"code,omitempty"`
}

func (t ASTEndpoint) GetType() NodeType {
	return NodeTypeEndpoint
}

func (e ASTEndpoint) GetChildren() []Node {
	return nil
}
