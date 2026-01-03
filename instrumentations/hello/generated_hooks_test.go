package generated_hooks

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pdelewski/go-build-interceptor/hooks"
)

// MockHookContext implements hooks.HookContext for testing
type MockHookContext struct {
	data        interface{}
	keyData     map[string]interface{}
	skipCall    bool
	funcName    string
	packageName string
}

func NewMockHookContext(packageName, funcName string) *MockHookContext {
	return &MockHookContext{
		keyData:     make(map[string]interface{}),
		funcName:    funcName,
		packageName: packageName,
	}
}

func (m *MockHookContext) SetData(data interface{}) {
	m.data = data
}

func (m *MockHookContext) GetData() interface{} {
	return m.data
}

func (m *MockHookContext) SetKeyData(key string, val interface{}) {
	m.keyData[key] = val
}

func (m *MockHookContext) GetKeyData(key string) interface{} {
	return m.keyData[key]
}

func (m *MockHookContext) HasKeyData(key string) bool {
	_, ok := m.keyData[key]
	return ok
}

func (m *MockHookContext) SetSkipCall(skip bool) {
	m.skipCall = skip
}

func (m *MockHookContext) IsSkipCall() bool {
	return m.skipCall
}

func (m *MockHookContext) GetFuncName() string {
	return m.funcName
}

func (m *MockHookContext) GetPackageName() string {
	return m.packageName
}

// Verify MockHookContext implements hooks.HookContext
var _ hooks.HookContext = (*MockHookContext)(nil)

// captureOutput captures stdout during a function call
func captureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// TestProvideHooks tests that ProvideHooks returns the correct hook definitions
func TestProvideHooks(t *testing.T) {
	hooksSlice := ProvideHooks()

	if len(hooksSlice) != 4 {
		t.Errorf("Expected 4 hooks, got %d", len(hooksSlice))
	}

	// Define expected hooks
	expectedHooks := []struct {
		pkg      string
		function string
		receiver string
		before   string
		after    string
		from     string
	}{
		{"main", "foo", "", "BeforeFoo", "AfterFoo", "generated_hooks"},
		{"main", "bar1", "", "BeforeBar1", "AfterBar1", "generated_hooks"},
		{"main", "bar2", "", "BeforeBar2", "AfterBar2", "generated_hooks"},
		{"main", "main", "", "BeforeMain", "AfterMain", "generated_hooks"},
	}

	for i, expected := range expectedHooks {
		if i >= len(hooksSlice) {
			t.Fatalf("Missing hook at index %d", i)
		}

		hook := hooksSlice[i]

		if hook.Target.Package != expected.pkg {
			t.Errorf("Hook %d: expected package %q, got %q", i, expected.pkg, hook.Target.Package)
		}

		if hook.Target.Function != expected.function {
			t.Errorf("Hook %d: expected function %q, got %q", i, expected.function, hook.Target.Function)
		}

		if hook.Target.Receiver != expected.receiver {
			t.Errorf("Hook %d: expected receiver %q, got %q", i, expected.receiver, hook.Target.Receiver)
		}

		if hook.Hooks == nil {
			t.Fatalf("Hook %d: Hooks is nil", i)
		}

		if hook.Hooks.Before != expected.before {
			t.Errorf("Hook %d: expected before %q, got %q", i, expected.before, hook.Hooks.Before)
		}

		if hook.Hooks.After != expected.after {
			t.Errorf("Hook %d: expected after %q, got %q", i, expected.after, hook.Hooks.After)
		}

		if hook.Hooks.From != expected.from {
			t.Errorf("Hook %d: expected from %q, got %q", i, expected.from, hook.Hooks.From)
		}
	}
}

// TestProvideHooksValidation tests that all returned hooks are valid
func TestProvideHooksValidation(t *testing.T) {
	hooksSlice := ProvideHooks()

	for i, hook := range hooksSlice {
		if err := hook.Validate(); err != nil {
			t.Errorf("Hook %d failed validation: %v", i, err)
		}
	}
}

// TestBeforeFoo tests the BeforeFoo hook function
func TestBeforeFoo(t *testing.T) {
	ctx := NewMockHookContext("main", "foo")

	output := captureOutput(func() {
		BeforeFoo(ctx)
	})

	// Check that startTime was set
	if !ctx.HasKeyData("startTime") {
		t.Error("Expected startTime to be set in context")
	}

	startTime, ok := ctx.GetKeyData("startTime").(time.Time)
	if !ok {
		t.Error("startTime should be of type time.Time")
	}

	if time.Since(startTime) > time.Second {
		t.Error("startTime should be recent")
	}

	// Check output
	if !strings.Contains(output, "[BEFORE]") {
		t.Errorf("Expected output to contain '[BEFORE]', got: %s", output)
	}

	if !strings.Contains(output, "main.foo()") {
		t.Errorf("Expected output to contain 'main.foo()', got: %s", output)
	}
}

// TestAfterFoo tests the AfterFoo hook function
func TestAfterFoo(t *testing.T) {
	ctx := NewMockHookContext("main", "foo")

	// Simulate BeforeFoo setting the startTime
	ctx.SetKeyData("startTime", time.Now().Add(-100*time.Millisecond))

	output := captureOutput(func() {
		AfterFoo(ctx)
	})

	// Check output
	if !strings.Contains(output, "[AFTER]") {
		t.Errorf("Expected output to contain '[AFTER]', got: %s", output)
	}

	if !strings.Contains(output, "main.foo()") {
		t.Errorf("Expected output to contain 'main.foo()', got: %s", output)
	}

	if !strings.Contains(output, "completed in") {
		t.Errorf("Expected output to contain 'completed in', got: %s", output)
	}
}

// TestAfterFooWithoutStartTime tests AfterFoo when startTime is not set
func TestAfterFooWithoutStartTime(t *testing.T) {
	ctx := NewMockHookContext("main", "foo")

	output := captureOutput(func() {
		AfterFoo(ctx)
	})

	// Should not print anything if startTime is not set
	if output != "" {
		t.Errorf("Expected no output when startTime is not set, got: %s", output)
	}
}

// TestBeforeBar1 tests the BeforeBar1 hook function
func TestBeforeBar1(t *testing.T) {
	ctx := NewMockHookContext("main", "bar1")

	output := captureOutput(func() {
		BeforeBar1(ctx)
	})

	if !ctx.HasKeyData("startTime") {
		t.Error("Expected startTime to be set in context")
	}

	if !strings.Contains(output, "[BEFORE]") || !strings.Contains(output, "main.bar1()") {
		t.Errorf("Unexpected output: %s", output)
	}
}

// TestAfterBar1 tests the AfterBar1 hook function
func TestAfterBar1(t *testing.T) {
	ctx := NewMockHookContext("main", "bar1")
	ctx.SetKeyData("startTime", time.Now().Add(-50*time.Millisecond))

	output := captureOutput(func() {
		AfterBar1(ctx)
	})

	if !strings.Contains(output, "[AFTER]") || !strings.Contains(output, "main.bar1()") {
		t.Errorf("Unexpected output: %s", output)
	}
}

// TestBeforeBar2 tests the BeforeBar2 hook function
func TestBeforeBar2(t *testing.T) {
	ctx := NewMockHookContext("main", "bar2")

	output := captureOutput(func() {
		BeforeBar2(ctx)
	})

	if !ctx.HasKeyData("startTime") {
		t.Error("Expected startTime to be set in context")
	}

	if !strings.Contains(output, "[BEFORE]") || !strings.Contains(output, "main.bar2()") {
		t.Errorf("Unexpected output: %s", output)
	}
}

// TestAfterBar2 tests the AfterBar2 hook function
func TestAfterBar2(t *testing.T) {
	ctx := NewMockHookContext("main", "bar2")
	ctx.SetKeyData("startTime", time.Now().Add(-50*time.Millisecond))

	output := captureOutput(func() {
		AfterBar2(ctx)
	})

	if !strings.Contains(output, "[AFTER]") || !strings.Contains(output, "main.bar2()") {
		t.Errorf("Unexpected output: %s", output)
	}
}

// TestBeforeMain tests the BeforeMain hook function
func TestBeforeMain(t *testing.T) {
	ctx := NewMockHookContext("main", "main")

	output := captureOutput(func() {
		BeforeMain(ctx)
	})

	if !ctx.HasKeyData("startTime") {
		t.Error("Expected startTime to be set in context")
	}

	if !strings.Contains(output, "[BEFORE]") || !strings.Contains(output, "main.main()") {
		t.Errorf("Unexpected output: %s", output)
	}
}

// TestAfterMain tests the AfterMain hook function
func TestAfterMain(t *testing.T) {
	ctx := NewMockHookContext("main", "main")
	ctx.SetKeyData("startTime", time.Now().Add(-200*time.Millisecond))

	output := captureOutput(func() {
		AfterMain(ctx)
	})

	if !strings.Contains(output, "[AFTER]") || !strings.Contains(output, "main.main()") {
		t.Errorf("Unexpected output: %s", output)
	}

	if !strings.Contains(output, "completed in") {
		t.Errorf("Expected output to contain duration, got: %s", output)
	}
}

// TestBeforeAfterIntegration tests a complete Before/After cycle
func TestBeforeAfterIntegration(t *testing.T) {
	ctx := NewMockHookContext("main", "foo")

	// Call BeforeFoo
	beforeOutput := captureOutput(func() {
		BeforeFoo(ctx)
	})

	// Simulate some work
	time.Sleep(10 * time.Millisecond)

	// Call AfterFoo
	afterOutput := captureOutput(func() {
		AfterFoo(ctx)
	})

	// Verify both outputs
	if !strings.Contains(beforeOutput, "[BEFORE]") {
		t.Error("Before hook should print [BEFORE]")
	}

	if !strings.Contains(afterOutput, "[AFTER]") {
		t.Error("After hook should print [AFTER]")
	}

	if !strings.Contains(afterOutput, "completed in") {
		t.Error("After hook should print duration")
	}
}

// TestMockHookContextSetData tests the SetData/GetData methods
func TestMockHookContextSetData(t *testing.T) {
	ctx := NewMockHookContext("pkg", "func")

	ctx.SetData("test data")
	if ctx.GetData() != "test data" {
		t.Errorf("Expected 'test data', got %v", ctx.GetData())
	}

	ctx.SetData(42)
	if ctx.GetData() != 42 {
		t.Errorf("Expected 42, got %v", ctx.GetData())
	}
}

// TestMockHookContextKeyData tests the SetKeyData/GetKeyData/HasKeyData methods
func TestMockHookContextKeyData(t *testing.T) {
	ctx := NewMockHookContext("pkg", "func")

	// Test HasKeyData before setting
	if ctx.HasKeyData("nonexistent") {
		t.Error("HasKeyData should return false for nonexistent key")
	}

	// Test SetKeyData and GetKeyData
	ctx.SetKeyData("key1", "value1")
	ctx.SetKeyData("key2", 123)

	if !ctx.HasKeyData("key1") {
		t.Error("HasKeyData should return true for existing key")
	}

	if ctx.GetKeyData("key1") != "value1" {
		t.Errorf("Expected 'value1', got %v", ctx.GetKeyData("key1"))
	}

	if ctx.GetKeyData("key2") != 123 {
		t.Errorf("Expected 123, got %v", ctx.GetKeyData("key2"))
	}

	// Test GetKeyData for nonexistent key
	if ctx.GetKeyData("nonexistent") != nil {
		t.Error("GetKeyData should return nil for nonexistent key")
	}
}

// TestMockHookContextSkipCall tests the SetSkipCall/IsSkipCall methods
func TestMockHookContextSkipCall(t *testing.T) {
	ctx := NewMockHookContext("pkg", "func")

	// Default should be false
	if ctx.IsSkipCall() {
		t.Error("IsSkipCall should default to false")
	}

	// Set to true
	ctx.SetSkipCall(true)
	if !ctx.IsSkipCall() {
		t.Error("IsSkipCall should return true after SetSkipCall(true)")
	}

	// Set back to false
	ctx.SetSkipCall(false)
	if ctx.IsSkipCall() {
		t.Error("IsSkipCall should return false after SetSkipCall(false)")
	}
}

// TestMockHookContextGetters tests the GetFuncName and GetPackageName methods
func TestMockHookContextGetters(t *testing.T) {
	ctx := NewMockHookContext("mypackage", "myfunction")

	if ctx.GetPackageName() != "mypackage" {
		t.Errorf("Expected 'mypackage', got %s", ctx.GetPackageName())
	}

	if ctx.GetFuncName() != "myfunction" {
		t.Errorf("Expected 'myfunction', got %s", ctx.GetFuncName())
	}
}