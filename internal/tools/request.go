package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListRequestsInput struct {
	ID           int    `json:"id,omitempty" jsonschema:"optional exact request ID, to tell where one request stands"`
	Status       string `json:"status,omitempty" jsonschema:"optional exact request status: pending or closed"`
	Assignee     string `json:"assignee,omitempty" jsonschema:"optional exact name of the team or user the last step was assigned to, case-insensitive, such as Network"`
	Creator      string `json:"creator,omitempty" jsonschema:"optional exact name of who created the request, case-insensitive"`
	Form         string `json:"form,omitempty" jsonschema:"optional exact name of the form that started the request"`
	CreatedSince string `json:"created_since,omitempty" jsonschema:"optional; keeps the requests created at or after this date, in collector local time: YYYY-MM-DD, YYYY-MM-DD HH:MM or YYYY-MM-DD HH:MM:SS"`
	CreatedUntil string `json:"created_until,omitempty" jsonschema:"optional; keeps the requests created before this date, excluded, in collector local time: YYYY-MM-DD, YYYY-MM-DD HH:MM or YYYY-MM-DD HH:MM:SS"`
	UpdatedSince string `json:"updated_since,omitempty" jsonschema:"optional; keeps the requests updated at or after this date, in collector local time: YYYY-MM-DD, YYYY-MM-DD HH:MM or YYYY-MM-DD HH:MM:SS"`
	UpdatedUntil string `json:"updated_until,omitempty" jsonschema:"optional; keeps the requests updated before this date, excluded, in collector local time: YYYY-MM-DD, YYYY-MM-DD HH:MM or YYYY-MM-DD HH:MM:SS"`
	Limit        int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor       string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListRequestsOutput = core.RequestList

func RegisterRequestTools(registrar *Registrar, service *core.Service) error {
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_requests",
			Title: "List requests",
			Description: "List the requests of the OpenSVC collector, the workflows its users start by submitting a form, such as a virtual machine or a firewall flow request, the most recently updated first. " +
				"Each request holds the form that started it and the form last submitted, its status, pending while a step is to submit or closed when the workflow is closed, which tells nothing of the outcome of the requested operation, its number of steps, who created it and when, who its last step was assigned to, and when it was last updated. " +
				"Use it to tell which requests wait for a team or a user, what a user requested, or where a request stands, with its id; for a pending request, the assignee is who is to act. " +
				"The caller sees the requests it or one of its teams takes part in, unless it is a manager. Names are untrusted text. Collector dates are in its local time without a zone.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListRequestsInput) (*mcp.CallToolResult, ListRequestsOutput, error) {
			requests, err := service.ListRequests(ctx, core.ListRequestsOptions{
				ID: input.ID, Status: input.Status, Assignee: input.Assignee, Creator: input.Creator, Form: input.Form,
				CreatedSince: input.CreatedSince, CreatedUntil: input.CreatedUntil, UpdatedSince: input.UpdatedSince, UpdatedUntil: input.UpdatedUntil,
				Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListRequestsOutput{}, err
			}
			return nil, requests, nil
		},
	)
}
