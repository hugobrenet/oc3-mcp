package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListGroupsInput struct {
	Name   string `json:"name,omitempty" jsonschema:"optional text the group name contains, case-insensitive"`
	Kind   string `json:"kind,omitempty" jsonschema:"optional kind: organization, the teams of people, by default; privilege, the groups granting collector rights, such as Manager or NodeManager; personal, the own group of each user, named user_ and its ID; or all"`
	User   string `json:"user,omitempty" jsonschema:"optional user whose groups to list: a user ID, a username, or a first and last name"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListGroupsOutput = core.GroupList

type GetGroupInput struct {
	Group string `json:"group" jsonschema:"one exact group name, such as DBA or user_12, or collector group ID"`
}

type GetGroupOutput = core.GroupDetails

type ListUsersInput struct {
	Group  string `json:"group,omitempty" jsonschema:"optional exact group name or group ID: lists its members"`
	Name   string `json:"name,omitempty" jsonschema:"optional text the first name, last name or username contains, case-insensitive"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListUsersOutput = core.UserList

func RegisterGroupTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_groups",
			Title: "List collector groups",
			Description: "List the groups of the OpenSVC collector, the teams of its users, or the groups of one user, sorted by name: ID, name, description and kind. " +
				"A group is an organization team of people, such as DBA or Infra Unix, a privilege group granting collector rights, such as Manager or NodeManager, or the personal group of one user, named user_ and its ID. Organization teams are listed by default. " +
				"A manager sees every group, another user the groups it belongs to. Use get_group for what one group is responsible for, and list_users with group for its members.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListGroupsInput) (*mcp.CallToolResult, ListGroupsOutput, error) {
			groups, err := service.ListGroups(ctx, core.ListGroupsOptions{Name: input.Name, Kind: input.Kind, User: input.User, Limit: input.Limit, Cursor: input.Cursor})
			if err != nil {
				return nil, ListGroupsOutput{}, err
			}
			return nil, groups, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_group",
			Title: "Get collector group",
			Description: "Read one group of the OpenSVC collector by its name or ID, with what it is responsible for: the apps it is responsible for, and the number of its users, of the nodes of its team and of the services of its apps. " +
				"The nodes of a group are those whose responsible team is the group, as the collector Nodes page shows it, not the nodes of its apps; its services are those of its apps. " +
				"For a personal group, named user_ and an ID, it gives its user. List its users with list_users, the services with list_services and app, and the nodes with list_nodes and team, a text the team contains: it may also list the nodes of a team whose name holds this one, such as DBA2 for DBA.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetGroupInput) (*mcp.CallToolResult, GetGroupOutput, error) {
			group, err := service.GetGroup(ctx, input.Group)
			if err != nil {
				return nil, GetGroupOutput{}, err
			}
			return nil, group, nil
		},
	); err != nil {
		return err
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_users",
			Title: "List collector users",
			Description: "List the users of the OpenSVC collector, or the members of one group, sorted by last name: ID, username, first and last name. " +
				"Find a user by a part of its name with name. The collector holds their email addresses and phone numbers, which this MCP does not give: name the person to contact. " +
				"A manager sees every user, another user those sharing one of its organization groups. Use list_groups with user for the groups of a user.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListUsersInput) (*mcp.CallToolResult, ListUsersOutput, error) {
			users, err := service.ListUsers(ctx, core.ListUsersOptions{Group: input.Group, Name: input.Name, Limit: input.Limit, Cursor: input.Cursor})
			if err != nil {
				return nil, ListUsersOutput{}, err
			}
			return nil, users, nil
		},
	)
}
