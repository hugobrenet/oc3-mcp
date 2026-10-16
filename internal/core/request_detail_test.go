package core

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// requestCollector answers a request with the stored forms given, the
// form_data of each written as JSON text.
func requestCollector(t *testing.T, steps ...map[string]any) *fakeCollector {
	t.Helper()
	collector := &fakeCollector{}
	collector.answer = func(path string, query url.Values) string {
		switch path {
		case "/workflows":
			return `{"data":[{"id":1355,"form_head_id":1412,"form_name":"sample-firewall-flow","status":"closed","steps":2,"creator":" Manager"}],"meta":{"total":1}}`
		case "/forms_store":
			offset := query.Get("offset")
			limit := query.Get("limit")
			var page []map[string]any
			start, _ := json.Number(offset).Int64()
			count, _ := json.Number(limit).Int64()
			for i := start; i < int64(len(steps)) && i < start+count; i++ {
				page = append(page, steps[i])
			}
			data, _ := json.Marshal(map[string]any{"data": page, "meta": map[string]any{"total": len(steps)}})
			return string(data)
		}
		return `{}`
	}
	return collector
}

func step(form string, data any, inputs ...map[string]any) map[string]any {
	text, ok := data.(string)
	if !ok {
		raw, _ := json.Marshal(data)
		text = string(raw)
	}
	return map[string]any{"form_name": form, "form_submitter": " Manager", "form_assignee": "Network", "form_data": text,
		"form_definition": map[string]any{"Label": form + " title", "Inputs": inputs}}
}

func values(t *testing.T, request RequestDetails, step int) map[string]RequestValue {
	t.Helper()
	got := map[string]RequestValue{}
	for _, value := range request.Steps[step].Values {
		got[value.Input] = value
	}
	return got
}

func TestGetRequestReadsItsStepsByInputKeyAndLabel(t *testing.T) {
	collector := requestCollector(t,
		step("sample-firewall-flow", map[string]any{"source": "web-12", "port": json.Number("5432"), "value": "s3cret", "extra": "x"},
			map[string]any{"Id": "source", "Label": "Source"}, map[string]any{"Id": "port", "Label": "Port"},
			// The collector reads an input by its Key when it has one.
			map[string]any{"Id": "password", "Key": "value", "Type": "password"}),
		step("sample-firewall-review", map[string]any{"decision": "approved"}, map[string]any{"Id": "decision", "Label": "Decision"}))
	request, err := newTestService(collector).GetRequest(t.Context(), GetRequestOptions{ID: 1355})
	if err != nil {
		t.Fatal(err)
	}
	if collector.requests[0].query.Get("filter") != "id:eq:1355" || collector.requests[1].query.Get("filter") != "form_head_id:eq:1412" ||
		collector.requests[1].query.Get("limit") != "20" || request.TotalSteps != 2 || request.Truncated || request.Request.Creator != "Manager" {
		t.Fatalf("request = %+v, %+v", request, collector.requests)
	}
	first := request.Steps[0]
	if first.Step != 1 || first.Label != "sample-firewall-flow title" || first.Submitter != "Manager" || first.Assignee != "Network" ||
		first.Values[0].Input != "source" || first.Values[1].Input != "port" || first.Values[2].Input != "value" || first.Values[3].Input != "extra" {
		t.Fatalf("first step = %+v", first)
	}
	got := values(t, request, 0)
	if got["port"].Value != "5432" || got["source"].Label != "Source" || !got["value"].Masked || got["value"].Value != "(masked)" || got["extra"].Masked {
		t.Fatalf("values = %+v", got)
	}
	if request.Steps[1].Step != 2 || values(t, request, 1)["decision"].Value != "approved" {
		t.Fatalf("second step = %+v", request.Steps[1])
	}
}

func TestGetRequestMasksNestedSecretsAndEmailAddresses(t *testing.T) {
	collector := requestCollector(t, step("vm", map[string]any{
		"settings":        map[string]any{"password": "secret", "hostname": "web-12", "users": []any{map[string]any{"apiKey": "k", "name": "bob"}}},
		"contact":         "Alice <alice@example.com>",
		"requestor_email": "a@b.local",
		"private_ip":      "10.0.0.1",
		"bypass":          "no",
	}))
	request, err := newTestService(collector).GetRequest(t.Context(), GetRequestOptions{ID: 1355})
	if err != nil {
		t.Fatal(err)
	}
	got := values(t, request, 0)
	settings := got["settings"]
	if !settings.Masked || strings.Contains(settings.Value, "secret") || !strings.Contains(settings.Value, "web-12") || !strings.Contains(settings.Value, "bob") ||
		strings.Contains(settings.Value, `"k"`) || !strings.Contains(settings.Value, `"apiKey":"(masked)"`) {
		t.Fatalf("settings = %+v", settings)
	}
	if !got["contact"].Masked || !got["requestor_email"].Masked || got["private_ip"].Masked || got["private_ip"].Value != "10.0.0.1" || got["bypass"].Masked {
		t.Fatalf("values = %+v", got)
	}
}

func TestGetRequestReadsListSubmissionsAndTellsUnreadableData(t *testing.T) {
	collector := requestCollector(t,
		step("vms", []any{map[string]any{"hostname": "web-12"}, map[string]any{"hostname": "web-13"}}, map[string]any{"Id": "hostname", "Label": "Hostname"}),
		step("broken", "{not json"),
		step("empty", ""))
	request, err := newTestService(collector).GetRequest(t.Context(), GetRequestOptions{ID: 1355})
	if err != nil {
		t.Fatal(err)
	}
	list := request.Steps[0].Values
	if len(list) != 2 || list[0].Entry != 1 || list[0].Value != "web-12" || list[1].Entry != 2 || list[1].Value != "web-13" || list[1].Label != "Hostname" {
		t.Fatalf("list values = %+v", list)
	}
	if request.Steps[1].DataError == "" || request.Steps[1].Values != nil || request.Steps[2].DataError != "" || request.Steps[2].Values != nil {
		t.Fatalf("broken and empty steps = %+v", request.Steps[1:])
	}
}

func TestGetRequestBoundsItsSizeAndContinuesWithTheCursor(t *testing.T) {
	big := map[string]any{}
	for i := range 150 {
		big["field"+strings.Repeat("x", 3)+string(rune('a'+i%26))+string(rune('a'+i/26))] = strings.Repeat("v", 2000)
	}
	var steps []map[string]any
	for range 4 {
		steps = append(steps, step("big", big))
	}
	collector := requestCollector(t, steps...)
	request, err := newTestService(collector).GetRequest(t.Context(), GetRequestOptions{ID: 1355})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(request)
	first := request.Steps[0]
	if len(data) > 300<<10 || len(request.Steps) != 2 || first.ValuesOmitted != 50 || len(first.Values) != 100 || !first.Values[0].Truncated ||
		!request.Truncated || request.NextCursor == "" {
		t.Fatalf("%d bytes, %d steps, first %d values %d omitted, cursor %q", len(data), len(request.Steps), len(first.Values), first.ValuesOmitted, request.NextCursor)
	}
	next, err := newTestService(collector).GetRequest(t.Context(), GetRequestOptions{ID: 1355, Cursor: request.NextCursor})
	if err != nil || next.Steps[0].Step != len(request.Steps)+1 {
		t.Fatalf("next = %+v, %v", next.Steps, err)
	}
	if _, err := newTestService(collector).GetRequest(t.Context(), GetRequestOptions{ID: 1356, Cursor: request.NextCursor}); err == nil {
		t.Fatal("a cursor of another request was accepted")
	}
}

func TestGetRequestRefusesAnUnknownOrInvalidID(t *testing.T) {
	collector := &fakeCollector{body: `{"data":[],"meta":{"total":0}}`}
	for _, options := range []GetRequestOptions{{}, {ID: 1, Limit: 51}, {ID: 1, Cursor: "garbage"}} {
		if _, err := newTestService(collector).GetRequest(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
	if _, err := newTestService(collector).GetRequest(t.Context(), GetRequestOptions{ID: 99}); err == nil || !strings.Contains(err.Error(), "use list_requests") {
		t.Fatalf("unknown request: %v", err)
	}
}

func TestSensitiveNamesAreWholeWords(t *testing.T) {
	for name, want := range map[string]bool{
		"password": true, "admin_password": true, "passwd": true, "Pass": true, "apiKey": true, "API key": true, "secretKey": true,
		"requestor_email": true, "e-mail": true, "token": true, "private_key": true, "credentials": true,
		"private_ip": false, "private_network": false, "bypass": false, "passive": false, "hostname": false, "mailbox_size": false, "tokenizer": false,
	} {
		if sensitiveName(name) != want {
			t.Errorf("%s: sensitive %v, want %v", name, !want, want)
		}
	}
}

// A step whose values alone exceed the budget is cut, alone in its page.
func TestGetRequestCutsAStepLargerThanThePage(t *testing.T) {
	big := map[string]any{}
	for i := range 100 {
		big["f"+string(rune('a'+i%26))+string(rune('a'+i/26))] = strings.Repeat("v", 4000)
	}
	var entries []any
	for range 3 {
		entries = append(entries, big)
	}
	request, err := newTestService(requestCollector(t, step("huge", entries))).GetRequest(t.Context(), GetRequestOptions{ID: 1355})
	if err != nil {
		t.Fatal(err)
	}
	first := request.Steps[0]
	if valuesSize(first.Values) > maxRequestValues || first.ValuesOmitted == 0 || len(first.Values)+first.ValuesOmitted != 300 || request.Truncated {
		t.Fatalf("%d values, %d bytes, %d omitted", len(first.Values), valuesSize(first.Values), first.ValuesOmitted)
	}
}

// The budget counts the values as they are encoded: a "<" or a control
// character takes 6 bytes in JSON, a quote 2.
func TestGetRequestBudgetCountsTheEncodedValues(t *testing.T) {
	escaped := map[string]any{}
	for i := range 100 {
		escaped["f"+string(rune('a'+i%26))+string(rune('a'+i/26))] = strings.Repeat("<\"\x01", 340)
	}
	var steps []map[string]any
	for range 3 {
		steps = append(steps, step("escaped", escaped))
	}
	request, err := newTestService(requestCollector(t, steps...)).GetRequest(t.Context(), GetRequestOptions{ID: 1355})
	if err != nil {
		t.Fatal(err)
	}
	encoded := 0
	for _, step := range request.Steps {
		data, _ := json.Marshal(step.Values)
		encoded += len(data)
	}
	if encoded > maxRequestValues+len(request.Steps)*2 || !request.Truncated && request.Steps[0].ValuesOmitted == 0 {
		t.Fatalf("%d steps hold %d encoded bytes, budget %d", len(request.Steps), encoded, maxRequestValues)
	}
}

// A label is checked whole: a sensitive word past the shown part still masks.
func TestGetRequestChecksTheWholeLabel(t *testing.T) {
	label := strings.Repeat("Account of the service owner, ", 12) + "password"
	request, err := newTestService(requestCollector(t, step("account", map[string]any{"field": "s3cret"},
		map[string]any{"Id": "field", "Label": label}))).GetRequest(t.Context(), GetRequestOptions{ID: 1355})
	if err != nil {
		t.Fatal(err)
	}
	value := request.Steps[0].Values[0]
	if !value.Masked || value.Value != "(masked)" || len(value.Label) > maxRequestLabel || strings.Contains(value.Label, "password") {
		t.Fatalf("value = %+v", value)
	}
}

// Of the inputs sharing a key, any sensitive one masks the value.
func TestGetRequestMasksAKeyAnySensitiveInputShares(t *testing.T) {
	request, err := newTestService(requestCollector(t, step("account", map[string]any{"value": "s3cret"},
		map[string]any{"Id": "normal", "Key": "value", "Type": "text", "Label": "Value"},
		map[string]any{"Id": "password", "Key": "value", "Type": "password"}))).GetRequest(t.Context(), GetRequestOptions{ID: 1355})
	if err != nil {
		t.Fatal(err)
	}
	values := request.Steps[0].Values
	if len(values) != 1 || !values[0].Masked || values[0].Value != "(masked)" || values[0].Label != "Value" {
		t.Fatalf("values = %+v", values)
	}
}

// Data after the first JSON value is an error, trailing spaces are not.
func TestGetRequestRefusesDataAfterTheValues(t *testing.T) {
	request, err := newTestService(requestCollector(t,
		step("garbage", `{"decision":"approved"} garbage`),
		step("two", `{"decision":"approved"} {"decision":"rejected"}`),
		step("spaces", "{\"decision\":\"approved\"} \n\t"))).GetRequest(t.Context(), GetRequestOptions{ID: 1355})
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{0, 1} {
		if request.Steps[i].DataError == "" || request.Steps[i].Values != nil {
			t.Fatalf("step %d = %+v", i+1, request.Steps[i])
		}
	}
	if request.Steps[2].DataError != "" || len(request.Steps[2].Values) != 1 {
		t.Fatalf("step with trailing spaces = %+v", request.Steps[2])
	}
}
