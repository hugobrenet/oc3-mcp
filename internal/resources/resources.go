// Package resources serves the MCP resources of the server: reference texts
// a client reads on demand, such as the format of the YAML of a form.
package resources

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// formDefinitionReference is the reference of the YAML of a form, read from
// the collector code, which the clients read before writing one.
//
//go:embed form_definition.md
var formDefinitionReference string

// FormDefinitionURI names the reference of the YAML of a form.
const FormDefinitionURI = "oc3://docs/form-definition"

// Register registers the resources on server, which an MCP client, such as
// the OpenSVC AI agent, reads on demand.
func Register(server *mcp.Server) (err error) {
	if server == nil {
		return fmt.Errorf("resource server is nil")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("register resource %q: %v", FormDefinitionURI, recovered)
		}
	}()
	server.AddResource(&mcp.Resource{
		URI:  FormDefinitionURI,
		Name: "form-definition", Title: "Form definition",
		Description: "The YAML of a form of the OpenSVC collector: its keys, what the collector checks at submission and what only the web interface reads, the conditions, the outputs by Dest, and examples. Read it before writing a form with create_form or update_form.",
		MIMEType:    "text/markdown",
		Size:        int64(len(formDefinitionReference)),
	}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI: FormDefinitionURI, MIMEType: "text/markdown", Text: formDefinitionReference,
		}}}, nil
	})
	return nil
}
