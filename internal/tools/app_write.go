package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type CreateAppInput struct {
	Name        string  `json:"name" jsonschema:"the app code, unique case-insensitively, as nodes and services will carry it"`
	Description *string `json:"description,omitempty" jsonschema:"optional description"`
	Domain      *string `json:"domain,omitempty" jsonschema:"optional domain"`
	TeamOps     *string `json:"team_ops,omitempty" jsonschema:"optional operations team, which should be the name of an organizational group"`
}

type UpdateAppInput struct {
	App         string  `json:"app" jsonschema:"one exact app code or app ID"`
	Name        *string `json:"name,omitempty" jsonschema:"optional new app code; the nodes and services carrying the app are renamed with it"`
	Description *string `json:"description,omitempty" jsonschema:"optional description; an empty string clears it"`
	Domain      *string `json:"domain,omitempty" jsonschema:"optional domain, at most 64 characters; an empty string clears it"`
	TeamOps     *string `json:"team_ops,omitempty" jsonschema:"optional operations team, the name of an organizational group; an empty string clears it"`
}

type DeleteAppInput struct {
	App string `json:"app" jsonschema:"one exact app code or app ID"`
}

type AppGroupsInput struct {
	App          string   `json:"app" jsonschema:"one exact app code or app ID"`
	Responsibles []string `json:"responsibles,omitempty" jsonschema:"groups, by exact name or ID, whose members manage the nodes and services of the app"`
	Publications []string `json:"publications,omitempty" jsonschema:"groups, by exact name or ID, whose members see the nodes and services of the app"`
}

type AppWriteOutput = core.AppWrite

type AppGroupsOutput = core.AppGroupChange

const appGroupRules = "One request per group, responsibles first, at most 50 groups; the outcome of each is given. The collector refuses a privilege group, such as Manager, and to a caller not a Manager a group they are not a member of. Requires the AppManager or the Manager privilege, and to be responsible for the app."

// registerAppWriteTools registers the tools that create, change and delete
// apps and their groups. RegisterNodeActionTools calls it: they are
// registered only when actions are enabled.
func registerAppWriteTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(registrar, &mcp.Tool{
		Name: "create_app", Title: "Create app",
		Description: "Create an app code in the OpenSVC collector, which nodes and services carry and through which groups are given the management and the visibility of them. The collector makes the default group of the caller responsible for it and publishes it to that group. " +
			"The app comes back read as get_app reads it. A code another app holds, whatever its case, is refused; the collector checks the other fields only at a change, with update_app, and stores them as given here. Requires the AppManager or the Manager privilege.",
		Annotations: actionAnnotations(false, false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateAppInput) (*mcp.CallToolResult, AppWriteOutput, error) {
		result, err := service.CreateApp(ctx, input.Name, core.AppFields{Description: input.Description, Domain: input.Domain, TeamOps: input.TeamOps})
		if err != nil {
			return nil, AppWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "update_app", Title: "Update app",
		Description: "Change the code, the description, the domain or the operations team of one app of the OpenSVC collector, the fields left out kept. A new code renames the app on its nodes and services at once. " +
			"The filtersets filtering on the old code, such as nodes.app or services.svc_app, keep it, and no longer select those nodes and services. " +
			"The app comes back read as get_app reads it; a change leaving it as it was is told unchanged. Requires the AppManager or the Manager privilege, and to be responsible for the app.",
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateAppInput) (*mcp.CallToolResult, AppWriteOutput, error) {
		result, err := service.UpdateApp(ctx, input.App, core.AppFields{Name: input.Name, Description: input.Description, Domain: input.Domain, TeamOps: input.TeamOps})
		if err != nil {
			return nil, AppWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "delete_app", Title: "Delete app",
		Description: "Delete one app code of the OpenSVC collector, with its responsible and publication groups. The collector refuses it while nodes or services carry it, which get_app counts. The result gives the app as it was. Requires the AppManager or the Manager privilege, and to be responsible for the app.",
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteAppInput) (*mcp.CallToolResult, AppWriteOutput, error) {
		result, err := service.DeleteApp(ctx, input.App)
		if err != nil {
			return nil, AppWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "attach_app_groups", Title: "Attach app groups",
		Description: "Make groups responsible for one app of the OpenSVC collector, whose members then manage its nodes and services, or publish it to groups, whose members then see them. A group already attached is told already_attached. " + appGroupRules,
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AppGroupsInput) (*mcp.CallToolResult, AppGroupsOutput, error) {
		result, err := service.AttachAppGroups(ctx, core.AppGroupOptions{App: input.App, Responsibles: input.Responsibles, Publications: input.Publications})
		if err != nil {
			return nil, AppGroupsOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	return addTool(registrar, &mcp.Tool{
		Name: "detach_app_groups", Title: "Detach app groups",
		Description: "Withdraw groups from the responsibles of one app of the OpenSVC collector, or unpublish it from groups: their members no longer manage, or see, its nodes and services through it. A group not attached is told already_detached; the responsibles left are read back, and whether the caller is still responsible. " + appGroupRules,
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AppGroupsInput) (*mcp.CallToolResult, AppGroupsOutput, error) {
		result, err := service.DetachAppGroups(ctx, core.AppGroupOptions{App: input.App, Responsibles: input.Responsibles, Publications: input.Publications})
		if err != nil {
			return nil, AppGroupsOutput{}, err
		}
		return nil, result, nil
	})
}
