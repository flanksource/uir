package databasecatalog

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/flanksource/commons-db/models"
	"github.com/flanksource/commons-db/query/schema"
	"github.com/flanksource/commons-db/types"
	"github.com/google/uuid"
)

var ConnectionTypes = []string{
	models.ConnectionTypeGit, models.ConnectionTypeGithub, models.ConnectionTypeAzureDevops,
	models.ConnectionTypeGitlab, models.ConnectionTypePostgres, models.ConnectionTypeSQLServer,
	models.ConnectionTypeSQLite, models.ConnectionTypeS3, models.ConnectionTypeMySQL,
}

const literalCredentialWarning = "Literal credentials are stored in the UIR database. Use a secret reference when possible."

// ConnectionInput is a patch: absent fields keep their saved value; an
// explicitly supplied empty string clears it.
type ConnectionInput struct {
	Name        *string            `json:"name"`
	Namespace   *string            `json:"namespace"`
	Type        *string            `json:"type"`
	URL         *string            `json:"url"`
	Username    *string            `json:"username"`
	Password    *string            `json:"password"`
	Certificate *string            `json:"certificate"`
	Properties  *map[string]string `json:"properties"`
	InsecureTLS *bool              `json:"insecure_tls"`
}

type ConnectionView struct {
	ID                 uuid.UUID         `json:"id"`
	Name               string            `json:"name"`
	Namespace          string            `json:"namespace"`
	Type               string            `json:"type"`
	URL                string            `json:"url,omitempty"`
	Username           string            `json:"username,omitempty"`
	Password           string            `json:"password,omitempty"`
	Certificate        string            `json:"certificate,omitempty"`
	Properties         map[string]string `json:"properties,omitempty"`
	InsecureTLS        bool              `json:"insecure_tls"`
	HasURL             bool              `json:"has_url"`
	HasUsername        bool              `json:"has_username"`
	HasPassword        bool              `json:"has_password"`
	HasCertificate     bool              `json:"has_certificate"`
	LiteralCredentials bool              `json:"literal_credentials"`
	Warnings           []string          `json:"warnings"`
}

func ApplyConnection(existing *models.Connection, input ConnectionInput) (models.Connection, error) {
	var model models.Connection
	if existing != nil {
		model = *existing
	}
	for _, field := range []struct{ into, from *string }{
		{&model.Name, input.Name}, {&model.Namespace, input.Namespace}, {&model.Type, input.Type},
		{&model.URL, input.URL}, {&model.Username, input.Username}, {&model.Password, input.Password}, {&model.Certificate, input.Certificate},
	} {
		if field.from != nil {
			*field.into = *field.from
		}
	}
	model.Name, model.Type = strings.TrimSpace(model.Name), strings.TrimSpace(model.Type)
	if input.Properties != nil {
		model.Properties = types.JSONStringMap(*input.Properties)
	}
	if input.InsecureTLS != nil {
		model.InsecureTLS = *input.InsecureTLS
	}
	if model.Name == "" || !slices.Contains(ConnectionTypes, model.Type) {
		return models.Connection{}, fmt.Errorf("name and one of the nine supported connection types are required")
	}
	if model.URL == "" && model.Type != models.ConnectionTypeGithub && model.Type != models.ConnectionTypeS3 {
		return models.Connection{}, fmt.Errorf("%s connection URL is required", model.Type)
	}
	if (model.Type == models.ConnectionTypeGithub || model.Type == models.ConnectionTypeGitlab || model.Type == models.ConnectionTypeAzureDevops) && model.Password == "" {
		return models.Connection{}, fmt.Errorf("%s access token is required", model.Type)
	}
	if len(model.Properties) > 0 {
		fields := schema.ConnectionComponents()[model.Type]["properties"].(schema.Schema)
		allowed := schema.Schema{}
		if nested, ok := fields["properties"].(schema.Schema); ok {
			allowed = nested["properties"].(schema.Schema)
		}
		for key, value := range model.Properties {
			if _, ok := allowed[key]; !ok {
				return models.Connection{}, fmt.Errorf("property %q is not supported for %s", key, model.Type)
			}
			if key == "usePathStyle" && value != "" {
				if _, err := strconv.ParseBool(value); err != nil {
					return models.Connection{}, fmt.Errorf("property usePathStyle must be true or false: %w", err)
				}
			}
		}
	}
	return model, nil
}

func ViewConnection(model models.Connection) ConnectionView {
	view := ConnectionView{
		ID: model.ID, Name: model.Name, Namespace: model.Namespace, Type: model.Type,
		Properties: map[string]string{}, InsecureTLS: model.InsecureTLS,
		HasURL: model.URL != "", HasUsername: model.Username != "", HasPassword: model.Password != "", HasCertificate: model.Certificate != "",
		Warnings: []string{},
	}
	for key, value := range model.Properties {
		if key == "region" || key == "assumeRole" || key == "bucket" || key == "usePathStyle" {
			view.Properties[key] = value
		}
	}
	if isReference(model.URL) || !urlContainsCredential(model.URL) {
		view.URL = model.URL
	}
	if isReference(model.Username) {
		view.Username = model.Username
	}
	if isReference(model.Password) {
		view.Password = model.Password
	}
	if isReference(model.Certificate) {
		view.Certificate = model.Certificate
	}
	view.LiteralCredentials = urlContainsCredential(model.URL) || literal(model.Password) || literal(model.Certificate) ||
		(model.Type == models.ConnectionTypeS3 && literal(model.Username))
	if view.LiteralCredentials {
		view.Warnings = append(view.Warnings, literalCredentialWarning)
	}
	return view
}

func isReference(value string) bool {
	for _, prefix := range []string{"op://", "secret://", "configmap://", "helm://", "serviceaccount://"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func literal(value string) bool { return value != "" && !isReference(value) }

func urlContainsCredential(value string) bool {
	endpoint, query, hasQuery := strings.Cut(value, "?")
	if strings.Contains(endpoint, "@") {
		return true
	}
	if !hasQuery {
		return false
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return true
	}
	for key := range values {
		key = strings.ToLower(key)
		for _, secret := range []string{"password", "pwd", "token", "secret", "signature", "credential", "api_key", "apikey", "access_key"} {
			if strings.Contains(key, secret) {
				return true
			}
		}
	}
	return false
}

// ConnectionSchemas projects the owning commons-db provider schemas for the
// local JsonSchemaForm. The Kubernetes picker hints and logging panel do not
// apply to the UIR browser; ordinary strings still accept literal values and
// secret references.
func ConnectionSchemas() map[string]schema.Schema {
	all := schema.ConnectionComponents()
	result := make(map[string]schema.Schema, len(ConnectionTypes))
	for _, typ := range ConnectionTypes {
		component := all[typ]
		fields := component["properties"].(schema.Schema)
		required := slices.Clone(component["required"].([]string))
		delete(fields, "namespace")
		delete(fields, "logging")
		delete(fields, "type")
		component["required"] = []string{"name"}
		for _, field := range required {
			if field != "type" && field != "name" {
				component["required"] = append(component["required"].([]string), field)
			}
		}
		cleanSchema(fields)
		result[typ] = component
	}
	return result
}

func cleanSchema(fields schema.Schema) {
	for _, value := range fields {
		field, ok := value.(schema.Schema)
		if !ok {
			continue
		}
		if widget, ok := field["x-clicky-component"].(string); ok && strings.HasPrefix(widget, "k8s-") {
			delete(field, "x-clicky-component")
			delete(field, "x-clicky-default-source")
		}
		if nested, ok := field["properties"].(schema.Schema); ok {
			cleanSchema(nested)
		}
	}
}
