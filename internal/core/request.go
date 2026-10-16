package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultListRequestsLimit = 50
	maxListRequestsLimit     = 200
)

var requestProps = []string{
	"id", "form_name", "form_folder", "last_form_name", "status", "steps",
	"creator", "create_date", "last_assignee", "last_update",
}

type ListRequestsOptions struct {
	ID           int
	Status       string
	Assignee     string
	Creator      string
	Form         string
	CreatedSince string
	CreatedUntil string
	UpdatedSince string
	UpdatedUntil string
	Limit        int
	Cursor       string
}

type RequestList struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Total      int        `json:"total" jsonschema:"the number of requests matching the filters, as the collector counts them"`
	Count      int        `json:"count" jsonschema:"the number of requests returned in this page"`
	Requests   []Request  `json:"requests" jsonschema:"the requests, the most recently updated first"`
	NextCursor string     `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page; it is no snapshot: a request updated between two pages moves to the front, and may be missed or listed twice"`
	Truncated  bool       `json:"truncated" jsonschema:"whether matching requests remain after this page"`
}

type Request struct {
	ID         int    `json:"id" jsonschema:"the collector request ID"`
	Form       string `json:"form,omitempty" jsonschema:"the form whose submission started the request"`
	FormFolder string `json:"form_folder,omitempty" jsonschema:"the folder of that form"`
	LastForm   string `json:"last_form,omitempty" jsonschema:"the form last submitted in the request"`
	Status     string `json:"status,omitempty" jsonschema:"the request status: pending while a step is to submit, closed when the workflow is closed, which tells nothing of the outcome of the requested operation"`
	Steps      int    `json:"steps" jsonschema:"the number of forms submitted in the request"`
	Creator    string `json:"creator,omitempty" jsonschema:"who submitted the first form"`
	Created    string `json:"created,omitempty" jsonschema:"when the request was created, as the collector gives it"`
	Assignee   string `json:"assignee,omitempty" jsonschema:"the team or user the last step was assigned to: for a pending request, who is to submit the next form"`
	Updated    string `json:"updated,omitempty" jsonschema:"when a form of the request was last submitted, as the collector gives it"`
}

type collectorRequest struct {
	ID           int    `json:"id"`
	FormName     string `json:"form_name"`
	FormFolder   string `json:"form_folder"`
	LastFormName string `json:"last_form_name"`
	Status       string `json:"status"`
	Steps        int    `json:"steps"`
	Creator      string `json:"creator"`
	CreateDate   string `json:"create_date"`
	LastAssignee string `json:"last_assignee"`
	LastUpdate   string `json:"last_update"`
}

// ListRequests reads one page of the requests: the workflows started by the
// submission of a form, the most recently updated first.
func (s *Service) ListRequests(ctx context.Context, options ListRequestsOptions) (RequestList, error) {
	for name, value := range map[string]string{"status": options.Status, "assignee": options.Assignee, "creator": options.Creator, "form": options.Form} {
		if err := validateFilterValue(name, value); err != nil {
			return RequestList{}, err
		}
	}
	for name, value := range map[string]string{"created_since": options.CreatedSince, "created_until": options.CreatedUntil, "updated_since": options.UpdatedSince, "updated_until": options.UpdatedUntil} {
		if err := validateCollectorDate(name, value); err != nil {
			return RequestList{}, err
		}
	}
	// The collector writes no other status: another one would match nothing.
	if options.Status != "" && options.Status != "pending" && options.Status != "closed" {
		return RequestList{}, fmt.Errorf("status must be pending or closed")
	}
	limit, err := pageLimit(options.Limit, defaultListRequestsLimit, maxListRequestsLimit)
	if err != nil {
		return RequestList{}, err
	}
	if options.ID < 0 {
		return RequestList{}, fmt.Errorf("id must be a positive request ID")
	}
	var filters []string
	if options.ID > 0 {
		filters = append(filters, filterEquals("id", strconv.Itoa(options.ID)))
	}
	if options.Status != "" {
		filters = append(filters, filterEquals("status", options.Status))
	}
	if options.Assignee != "" {
		filters = append(filters, filterEqualsTrimmed("last_assignee", options.Assignee))
	}
	if options.Creator != "" {
		filters = append(filters, filterEqualsTrimmed("creator", options.Creator))
	}
	if options.Form != "" {
		filters = append(filters, filterEquals("form_name", options.Form))
	}
	if options.CreatedSince != "" {
		filters = append(filters, "create_date:gte:"+options.CreatedSince)
	}
	if options.CreatedUntil != "" {
		filters = append(filters, "create_date:lt:"+options.CreatedUntil)
	}
	if options.UpdatedSince != "" {
		filters = append(filters, "last_update:gte:"+options.UpdatedSince)
	}
	if options.UpdatedUntil != "" {
		filters = append(filters, "last_update:lt:"+options.UpdatedUntil)
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return RequestList{}, err
	}

	var response collectorList[collectorRequest]
	if err := s.client.GetJSON(ctx, "/workflows", pageQuery(requestProps, "-last_update,-id", filters, limit, offset), &response); err != nil {
		return RequestList{}, fmt.Errorf("list requests: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return RequestList{}, fmt.Errorf("list requests: unexpected collector response")
	}
	result := RequestList{Provenance: s.newProvenance(), Total: *response.Meta.Total, Count: len(response.Data), Requests: make([]Request, 0, len(response.Data))}
	for _, row := range response.Data {
		result.Requests = append(result.Requests, requestOf(row))
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// requestOf turns a collector workflow row into a request.
func requestOf(row collectorRequest) Request {
	return Request{
		ID: row.ID, Form: row.FormName, FormFolder: row.FormFolder, LastForm: row.LastFormName, Status: row.Status, Steps: row.Steps,
		Creator: strings.TrimSpace(row.Creator), Created: row.CreateDate, Assignee: strings.TrimSpace(row.LastAssignee), Updated: row.LastUpdate,
	}
}
