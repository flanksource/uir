package uir

import (
	"encoding/json"
	"os/exec"
	"testing"
)

// TestPythonUIRMarshalUnmarshal tests that Python can generate UIR JSON that Go can unmarshal
func TestPythonUIRMarshalUnmarshal(t *testing.T) {
	// Execute Python script to generate JSON
	cmd := exec.Command("python3", "python/test_uir.py")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to execute Python script: %v", err)
	}

	// Unmarshal JSON into Go ModuleNode
	var modules []ModuleNode
	if err := json.Unmarshal(output, &modules); err != nil {
		t.Fatalf("failed to unmarshal Python JSON: %v\nOutput: %s", err, string(output))
	}

	// Verify structure
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}

	module := modules[0]
	if module.Module != "github.com/example/service" {
		t.Errorf("expected module 'github.com/example/service', got %s", module.Module)
	}

	if len(module.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(module.Packages))
	}

	pkg := module.Packages[0]
	if pkg.Package != "com.example.service" {
		t.Errorf("expected package 'com.example.service', got %s", pkg.Package)
	}

	if len(pkg.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(pkg.Types))
	}

	userService := pkg.Types[0]
	if userService.Type != "UserService" {
		t.Errorf("expected type 'UserService', got %s", userService.Type)
	}

	if len(userService.Methods) != 3 {
		t.Fatalf("expected 3 methods, got %d", len(userService.Methods))
	}

	// Verify GetUser method
	getUser := userService.Methods[0]
	if getUser.Method != "GetUser" {
		t.Errorf("expected method 'GetUser', got %s", getUser.Method)
	}

	if getUser.Body == nil {
		t.Fatal("GetUser should have a body")
	}

	if len(getUser.Body.Children) != 4 {
		t.Errorf("expected 4 statements in GetUser, got %d", len(getUser.Body.Children))
	}

	// Verify CreateUser method
	createUser := userService.Methods[1]
	if createUser.Method != "CreateUser" {
		t.Errorf("expected method 'CreateUser', got %s", createUser.Method)
	}

	if createUser.Body == nil {
		t.Fatal("CreateUser should have a body")
	}

	if len(createUser.Body.Children) != 1 {
		t.Errorf("expected 1 statement in CreateUser, got %d", len(createUser.Body.Children))
	}

	// Verify SearchUsers method
	searchUsers := userService.Methods[2]
	if searchUsers.Method != "SearchUsers" {
		t.Errorf("expected method 'SearchUsers', got %s", searchUsers.Method)
	}

	t.Logf("Successfully verified Python UIR implementation!")
	t.Logf("Module: %s", module.Module)
	t.Logf("Package: %s", pkg.Package)
	t.Logf("Type: %s", userService.Type)
	t.Logf("Methods: %s, %s, %s", getUser.Method, createUser.Method, searchUsers.Method)
}
