package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestDiscoveryMatchesRouter(t *testing.T) {
	r := fixtureRouter()
	w := extendedRequest(r, "GET", "/openapi.json", nil, nil)
	var spec struct {
		OpenAPI    string                                `json:"openapi"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components map[string]json.RawMessage            `json:"components"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || spec.OpenAPI != "3.1.0" {
		t.Fatal("invalid OpenAPI document")
	}
	inventory, err := routeInventory(r)
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]bool)
	for _, route := range inventory {
		path := routeVariable.ReplaceAllString(route.path, "{$1}")
		for _, method := range route.methods {
			key := strings.ToLower(method)
			if method == "CONNECT" {
				key = "x-connect"
			}
			operation, ok := spec.Paths[path][key]
			if !ok {
				t.Fatalf("OpenAPI omits %s %s", method, path)
			}
			var decoded struct {
				ID         string                `json:"operationId"`
				Responses  map[string]any        `json:"responses"`
				Security   []map[string][]string `json:"security"`
				Parameters []struct {
					Name, In string
					Required bool
				} `json:"parameters"`
			}
			if err := json.Unmarshal(operation, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.ID == "" || ids[decoded.ID] || len(decoded.Responses) == 0 {
				t.Fatalf("invalid operation %s %s", method, path)
			}
			ids[decoded.ID] = true
			if strings.HasPrefix(path, "/sessions/{id}") && (len(decoded.Security) != 1 || decoded.Security[0]["SessionBearer"] == nil) {
				t.Fatalf("missing session authentication: %s", path)
			}
			for _, variable := range routeVariable.FindAllStringSubmatch(route.path, -1) {
				found := false
				for _, parameter := range decoded.Parameters {
					if parameter.In == "path" && parameter.Name == variable[1] && parameter.Required {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing required path parameter %s on %s", variable[1], path)
				}
			}
		}
	}
	w = extendedRequest(r, "GET", "/capabilities", nil, nil)
	var capabilities struct {
		Router string         `json:"router"`
		Limits map[string]int `json:"limits"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &capabilities); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || capabilities.Router != "gorilla/mux" || capabilities.Limits["sessions"] != maxSessions || capabilities.Limits["expanded_body_bytes"] != maxBodyBytes {
		t.Fatal("capabilities disagree with runtime")
	}
	w = extendedRequest(r, "GET", "/collection.json", nil, nil)
	var collection struct {
		Info  map[string]string `json:"info"`
		Items []struct {
			Name    string         `json:"name"`
			Request map[string]any `json:"request"`
		} `json:"item"`
		Variables []map[string]string `json:"variable"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &collection); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(collection.Items) != len(ids) || len(collection.Variables) != 3 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("invalid Postman collection")
	}
	for _, item := range collection.Items {
		if strings.HasPrefix(item.Name, "POST /sessions/{id}") && item.Request["auth"] == nil {
			t.Fatal("collection omits session auth")
		}
	}
}

func TestDiscoveryConcurrentInitialization(t *testing.T) {
	r := fixtureRouter()
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for _, path := range []string{"/openapi.json", "/collection.json"} {
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				if w.Code != 200 || !json.Valid(w.Body.Bytes()) {
					t.Errorf("discovery initialization failed: %s", path)
				}
			}
		}()
	}
	group.Wait()
}

func TestSessionSchemaMatchesStatusValidation(t *testing.T) {
	document := openAPIDocument(nil)
	components := document["components"].(map[string]any)
	schema := components["schemas"].(map[string]any)["SessionConfiguration"].(map[string]any)
	sequence := schema["properties"].(map[string]any)["sequence"].(map[string]any)
	items := sequence["items"].(map[string]any)
	allowed := make(map[int]bool)
	for _, code := range items["enum"].([]int) {
		allowed[code] = true
	}
	for code := 200; code <= 599; code++ {
		if allowed[code] != (http.StatusText(code) != "") {
			t.Fatalf("schema/runtime status mismatch: %d", code)
		}
	}
	r, _ := sessionTestRouter()
	w := extendedRequest(r, "POST", "/sessions", nil, strings.NewReader(`{"sequence":[299]}`))
	if w.Code != 400 {
		t.Fatal("unrecognized status accepted")
	}
}
