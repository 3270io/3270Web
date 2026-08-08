package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jnnngs/3270Web/internal/mcptools"
)

const taskCatalogueRoute = "GET /api/v1/tasks"

// oneTaskCatalogue is a balance enquiry with one required, patterned
// parameter and one sensitive one — the two cases whose schema treatment
// differs.
const oneTaskCatalogue = `{"tasks":[{
  "name": "Account balance enquiry",
  "description": "Retrieves the current cleared balance for a customer account.",
  "source": "recording",
  "parameters": [
    {"name":"account_number","label":"Account number","required":true,"maxLength":8,"pattern":"\\d{8}","example":"12345678"},
    {"name":"pin","label":"PIN","required":true,"sensitive":true}
  ],
  "steps": [{"expect":[{"row":1,"column":1,"text":"ACCOUNT ENQUIRY"}],
             "inputs":[{"row":5,"column":20,"length":8,"parameter":"account_number"},
                       {"row":6,"column":20,"length":4,"parameter":"pin"}],
             "aidKey":"Enter"}],
  "outputs": [{"name":"cleared_balance","label":"Cleared balance","row":8,"column":20,"length":12}]
}]}`

const twoTaskCatalogue = `{"tasks":[
  {"name":"Account balance enquiry","steps":[{"aidKey":"Enter"}]},
  {"name":"Statement request","steps":[{"aidKey":"Enter"}]}
]}`

func taskInvoker(catalogue string) *fakeInvoker {
	inv := newFakeInvoker()
	inv.route(taskCatalogueRoute, jsonResponse(catalogue))
	return inv
}

// TestMCPTaskToolsAppearPerTask is the point of the feature: a saved task is
// in tools/list under its own name, with the parameters it declares, rather
// than hidden behind a generic runner the model has to know to ask about.
func TestMCPTaskToolsAppearPerTask(t *testing.T) {
	session := connectMCP(t, taskInvoker(oneTaskCatalogue), mcptools.TierInteract)
	tools := listToolNames(t, session)

	tool, ok := tools["task_account_balance_enquiry"]
	if !ok {
		t.Fatalf("the saved task should be offered as its own tool; got %v", toolNameList(tools))
	}
	if !strings.Contains(tool.Description, "cleared balance") &&
		!strings.Contains(tool.Description, "Cleared balance") {
		t.Errorf("the description should name what the task returns, got %q", tool.Description)
	}

	encoded, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("generated schema does not marshal: %v", err)
	}
	schema := string(encoded)
	for _, want := range []string{"account_number", "maxLength", `^(?:\\d{8})$`, "12345678", "writeOnly"} {
		if !strings.Contains(schema, want) {
			t.Errorf("generated schema is missing %q: %s", want, schema)
		}
	}

	// Generated from the declarations, so both parameters are required and
	// undeclared ones are refused — the same rules ResolveParameters applies
	// server-side.
	if !strings.Contains(schema, `"additionalProperties":false`) {
		t.Errorf("generated schema should refuse undeclared parameters: %s", schema)
	}

	// The generic pair stays for clients that do not act on list_changed.
	for _, want := range []string{"list_tasks", "run_task"} {
		if _, ok := tools[want]; !ok {
			t.Errorf("%q should still be offered alongside the generated tools", want)
		}
	}
}

// TestMCPTaskToolsRespectTheTier: reading the catalogue is reading, running a
// task types into a live application.
func TestMCPTaskToolsRespectTheTier(t *testing.T) {
	session := connectMCP(t, taskInvoker(oneTaskCatalogue), mcptools.TierRead)
	tools := listToolNames(t, session)

	if _, ok := tools["list_tasks"]; !ok {
		t.Error("list_tasks should be available at the readonly tier")
	}
	for _, absent := range []string{"run_task", "task_account_balance_enquiry"} {
		if _, ok := tools[absent]; ok {
			t.Errorf("%q must not be offered at the readonly tier", absent)
		}
	}

	// And the listing says why, rather than presenting tasks that cannot run
	// as though they could.
	text, isErr := callText(t, session, "list_tasks", nil)
	if isErr {
		t.Fatalf("list_tasks failed: %s", text)
	}
	if !strings.Contains(text, "MCP_TOOLS=interactive") {
		t.Errorf("the read-only listing should name the setting that enables running, got %q", text)
	}
}

// TestMCPTaskToolPostsTheDeclaredParameters checks the request that reaches
// the server, not only the reply.
func TestMCPTaskToolPostsTheDeclaredParameters(t *testing.T) {
	inv := taskInvoker(oneTaskCatalogue)
	inv.route("POST /api/v1/sessions/SESSION1/tasks/run", jsonResponse(`{
      "task":"Account balance enquiry","completed":true,"durationMs":812,
      "steps":[{"index":1,"status":"ok"}],
      "outputs":[{"name":"cleared_balance","label":"Cleared balance","value":"1,240.55 CR","found":true}]
    }`))
	session := connectMCP(t, inv, mcptools.TierInteract)

	if _, isErr := callText(t, session, "connect_session", map[string]any{"hostname": "example:23"}); isErr {
		t.Fatal("connect_session failed")
	}

	text, isErr := callText(t, session, "task_account_balance_enquiry", map[string]any{
		// Deliberately a JSON number: a model that reads "8 digits" often
		// sends one, and a type error it cannot act on would be a dead end.
		"account_number": 12345678,
		"pin":            "4321",
	})
	if isErr {
		t.Fatalf("running the task failed: %s", text)
	}

	body, _ := inv.body("POST /api/v1/sessions/SESSION1/tasks/run").(map[string]any)
	if body == nil {
		t.Fatal("the task run did not reach the server")
	}
	if body["name"] != "Account balance enquiry" {
		t.Errorf("name = %v, want the task's real name, not its tool name", body["name"])
	}
	params, _ := body["parameters"].(map[string]string)
	if params["account_number"] != "12345678" {
		t.Errorf("account_number = %q, want the number rendered as digits and not in exponent form", params["account_number"])
	}
	if params["pin"] != "4321" {
		t.Errorf("pin = %q, want it passed through to the host", params["pin"])
	}

	if !strings.Contains(text, "1,240.55 CR") {
		t.Errorf("the answer should carry the captured output, got %q", text)
	}
	// Outputs are read off a screen, so they are host data like any other.
	if !strings.Contains(text, "<untrusted-host-data>") {
		t.Errorf("a task result must be marked as host-derived, got %q", text)
	}
}

// TestMCPTaskFailureIsReportedNotRetried: a guard failing means the
// application was not where the task expected. Reporting it as a bare error
// invites the model to call again with the same arguments.
func TestMCPTaskFailureIsReportedNotRetried(t *testing.T) {
	inv := taskInvoker(oneTaskCatalogue)
	inv.route("POST /api/v1/sessions/SESSION1/tasks/run", jsonResponse(`{
      "task":"Account balance enquiry","completed":false,"durationMs":410,
      "steps":[{"index":1,"status":"failed"}],
      "failure":{"step":1,"reason":"the screen was not the one this step expects",
                 "expected":"ACCOUNT ENQUIRY at row 1 column 1","actual":"SIGN ON",
                 "screen":"SIGN ON\nUSERID:"}
    }`))
	session := connectMCP(t, inv, mcptools.TierInteract)
	if _, isErr := callText(t, session, "connect_session", map[string]any{"hostname": "example:23"}); isErr {
		t.Fatal("connect_session failed")
	}

	text, _ := callText(t, session, "task_account_balance_enquiry", map[string]any{
		"account_number": "12345678", "pin": "4321",
	})
	for _, want := range []string{"did NOT complete", "step 1", "ACCOUNT ENQUIRY", "SIGN ON", "Do not retry"} {
		if !strings.Contains(text, want) {
			t.Errorf("the failure report should contain %q, got %q", want, text)
		}
	}
}

// TestMCPTaskToolsFollowTheCatalogue is the tools/list_changed contract: a
// task saved from the browser mid-conversation becomes callable, and a
// deleted one stops being offered.
func TestMCPTaskToolsFollowTheCatalogue(t *testing.T) {
	inv := taskInvoker(oneTaskCatalogue)
	session := connectMCP(t, inv, mcptools.TierInteract)

	if _, ok := listToolNames(t, session)["task_statement_request"]; ok {
		t.Fatal("task_statement_request should not exist before it is saved")
	}

	inv.route(taskCatalogueRoute, jsonResponse(twoTaskCatalogue))
	if text, isErr := callText(t, session, "list_tasks", nil); isErr {
		t.Fatalf("list_tasks failed: %s", text)
	}

	tools := listToolNames(t, session)
	if _, ok := tools["task_statement_request"]; !ok {
		t.Errorf("a newly saved task should become callable; got %v", toolNameList(tools))
	}

	inv.route(taskCatalogueRoute, jsonResponse(`{"tasks":[]}`))
	if text, isErr := callText(t, session, "list_tasks", nil); isErr {
		t.Fatalf("list_tasks failed: %s", text)
	}
	tools = listToolNames(t, session)
	for _, gone := range []string{"task_statement_request", "task_account_balance_enquiry"} {
		if _, ok := tools[gone]; ok {
			t.Errorf("%q should have been withdrawn when it left the catalogue", gone)
		}
	}
	if _, ok := tools["run_task"]; !ok {
		t.Error("run_task must survive an empty catalogue")
	}
}

// TestMCPTaskListChangedIsNotified proves the removal and addition reach the
// client as a notification, which is what a client needs to re-list without
// polling.
func TestMCPTaskListChangedIsNotified(t *testing.T) {
	inv := taskInvoker(oneTaskCatalogue)

	changed := make(chan struct{}, 8)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := buildMCPServer(inv, mcptools.TierInteract)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	go func() {
		if err := server.Run(ctx, serverTransport); err != nil && ctx.Err() == nil {
			t.Errorf("server.Run: %v", err)
		}
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, &mcp.ClientOptions{
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
			select {
			case changed <- struct{}{}:
			default:
			}
		},
	})
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// Drain anything emitted while the tools were first registered.
	drain(changed)

	inv.route(taskCatalogueRoute, jsonResponse(twoTaskCatalogue))
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_tasks"}); err != nil {
		t.Fatalf("list_tasks: %v", err)
	}

	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("a catalogue change must notify the client, or it never re-lists")
	}

	// And the other half of the contract: reading a catalogue that has not
	// moved must be silent. A notification on every read would train a client
	// to ignore the one that matters.
	drain(changed)
	for i := 0; i < 3; i++ {
		if _, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_tasks"}); err != nil {
			t.Fatalf("list_tasks: %v", err)
		}
	}
	select {
	case <-changed:
		t.Error("an unchanged catalogue must not emit tools/list_changed")
	case <-time.After(250 * time.Millisecond):
	}
}

// TestMCPTaskToolNeedsASession — a task drives a terminal, so there has to be
// one, and the refusal has to say how to get one.
func TestMCPTaskToolNeedsASession(t *testing.T) {
	session := connectMCP(t, taskInvoker(oneTaskCatalogue), mcptools.TierInteract)

	text, isErr := callText(t, session, "task_account_balance_enquiry", map[string]any{
		"account_number": "12345678", "pin": "4321",
	})
	if !isErr {
		t.Error("running a task with no session should be an error")
	}
	if !strings.Contains(text, "connect_session") {
		t.Errorf("the refusal should say how to open a session, got %q", text)
	}
}

// TestMCPTaskCatalogueFailureIsSurvivable: the task catalogue is one route
// among many, and an instance that cannot serve it should still be drivable.
func TestMCPTaskCatalogueFailureIsSurvivable(t *testing.T) {
	inv := newFakeInvoker()
	inv.fail[taskCatalogueRoute] = mcptools.Response{
		Status: 500, ContentType: "application/json",
		Body: []byte(`{"error":"could not read the task catalogue"}`),
	}
	session := connectMCP(t, inv, mcptools.TierInteract)

	tools := listToolNames(t, session)
	if _, ok := tools["get_screen"]; !ok {
		t.Error("a broken task catalogue must not take the rest of the server with it")
	}

	text, isErr := callText(t, session, "list_tasks", nil)
	if !isErr {
		t.Error("list_tasks should report the failure rather than an empty catalogue")
	}
	if !strings.Contains(text, "task catalogue") {
		t.Errorf("the error should say what could not be read, got %q", text)
	}
}

func toolNameList(tools map[string]*mcp.Tool) []string {
	out := make([]string, 0, len(tools))
	for name := range tools {
		out = append(out, name)
	}
	return out
}

func drain(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
