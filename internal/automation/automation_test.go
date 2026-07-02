package automation

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"protean-provider/internal/domain"
)

const mockUIDump = `<?xml version="1.0" encoding="utf-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,1920]">
    <node index="0" class="android.widget.LinearLayout" bounds="[0,0][1080,1920]">
      <node index="0" resource-id="com.demo:id/username" class="android.widget.EditText" text="admin-user" bounds="[100,500][980,600]" clickable="true" enabled="true" />
      <node index="1" resource-id="com.demo:id/password" class="android.widget.EditText" text="" bounds="[100,650][980,750]" clickable="true" enabled="true" />
      <node index="2" resource-id="com.demo:id/login" content-desc="Submit Login" class="android.widget.Button" text="Login" bounds="[300,800][780,900]" clickable="true" enabled="true" />
    </node>
  </node>
</hierarchy>`

// mockDriver implements domain.Driver for execution testing.
type mockDriver struct {
	launchErr     error
	terminateErr  error
	tapErr        error
	swipeErr      error
	inputErr      error
	screenshotErr error
	dumpUIErr     error
	screenSizeErr error

	launchedApps   []string
	terminatedApps []string
	tappedCoords   [][2]float64
	inputsReceived []string
	swipesReceived []struct {
		sx, sy, ex, ey float64
		dur            int
	}

	dumpUIFn func() string
	tapFn    func(x, y float64) error
}

func (m *mockDriver) Launch(ctx context.Context, appID string) error {
	m.launchedApps = append(m.launchedApps, appID)
	return m.launchErr
}

func (m *mockDriver) Terminate(ctx context.Context, appID string) error {
	m.terminatedApps = append(m.terminatedApps, appID)
	return m.terminateErr
}

func (m *mockDriver) Tap(ctx context.Context, x, y float64) error {
	m.tappedCoords = append(m.tappedCoords, [2]float64{x, y})
	if m.tapFn != nil {
		return m.tapFn(x, y)
	}
	return m.tapErr
}

func (m *mockDriver) Swipe(ctx context.Context, startX, startY, endX, endY float64, durationMs int) error {
	m.swipesReceived = append(m.swipesReceived, struct {
		sx, sy, ex, ey float64
		dur            int
	}{startX, startY, endX, endY, durationMs})
	return m.swipeErr
}

func (m *mockDriver) Input(ctx context.Context, text string) error {
	m.inputsReceived = append(m.inputsReceived, text)
	return m.inputErr
}

func (m *mockDriver) Screenshot(ctx context.Context) ([]byte, error) {
	if m.screenshotErr != nil {
		return nil, m.screenshotErr
	}
	return []byte("mock-screenshot-bytes"), nil
}

func (m *mockDriver) DumpUI(ctx context.Context) (string, error) {
	if m.dumpUIErr != nil {
		return "", m.dumpUIErr
	}
	if m.dumpUIFn != nil {
		return m.dumpUIFn(), nil
	}
	return mockUIDump, nil
}

func (m *mockDriver) CurrentApp(ctx context.Context) (*domain.AppInfo, error) {
	return &domain.AppInfo{PackageName: "com.demo", Activity: ".MainActivity"}, nil
}

func (m *mockDriver) Install(ctx context.Context, filepath string) error {
	return nil
}

func (m *mockDriver) Uninstall(ctx context.Context, appID string) error {
	return nil
}

func (m *mockDriver) ScreenSize(ctx context.Context) (width, height int32, err error) {
	if m.screenSizeErr != nil {
		return 0, 0, m.screenSizeErr
	}
	return 1080, 1920, nil
}

// ------------------------------------------------
// Tests
// ------------------------------------------------

func TestNodeScoring(t *testing.T) {
	// Button should score much higher than a container layout
	button := &XMLNode{
		Class:      "android.widget.Button",
		Clickable:  "true",
		Enabled:    "true",
		ResourceID: "com.demo:id/login",
		Text:       "Login",
	}
	container := &XMLNode{
		Class:   "androidx.recyclerview.widget.RecyclerView",
		Enabled: "true",
		Bounds:  "[0,0][1080,1920]",
	}

	buttonScore := ScoreNode(button, 1080, 1920)
	containerScore := ScoreNode(container, 1080, 1920)

	if buttonScore <= containerScore {
		t.Errorf("Button (score=%d) should score higher than RecyclerView (score=%d)", buttonScore, containerScore)
	}

	// Verify container penalty applies
	if containerScore >= 0 {
		t.Errorf("RecyclerView should have negative or zero score, got %d", containerScore)
	}
}

func TestFindBestNodeAt_PrefersListItemOverRecyclerView(t *testing.T) {
	xmlHierarchy := `<?xml version="1.0" encoding="utf-8"?>
<hierarchy rotation="0">
  <node index="0" resource-id="com.demo:id/recycler_view" class="androidx.recyclerview.widget.RecyclerView" bounds="[0,200][1080,1920]" clickable="true" enabled="true">
    <node index="0" resource-id="com.demo:id/item_container" class="android.widget.LinearLayout" bounds="[0,200][1080,350]" enabled="true">
      <node index="0" resource-id="com.demo:id/title" class="android.widget.TextView" text="Bluetooth" bounds="[50,220][500,330]" enabled="true" />
    </node>
  </node>
</hierarchy>`

	// The click is inside the LinearLayout list item, but outside the TextView.
	// Click coordinate is (600, 270).
	x := 600.0 / 1080.0
	y := 270.0 / 1920.0

	node, _ := FindBestNodeAt(xmlHierarchy, x, y, 1080, 1920)
	if node == nil {
		t.Fatal("expected to find a node")
	}

	// Should pick the LinearLayout (item_container), NOT the RecyclerView
	if node.ResourceID != "com.demo:id/item_container" {
		t.Errorf("expected LinearLayout (com.demo:id/item_container), got %s (%s)", node.ResourceID, node.Class)
	}
}

func TestFindBestNodeAt_ScoresOverDeepest(t *testing.T) {
	// RecyclerView contains a LinearLayout which contains a TextView
	// Click coordinates are inside all three — but TextView should win by score
	overlappingXML := `<?xml version="1.0" encoding="utf-8"?>
<hierarchy rotation="0">
  <node index="0" resource-id="com.demo:id/recycler_view" class="androidx.recyclerview.widget.RecyclerView" bounds="[0,200][1080,1920]" enabled="true">
    <node index="0" resource-id="com.demo:id/item_container" class="android.widget.LinearLayout" bounds="[0,200][1080,350]" enabled="true">
      <node index="0" resource-id="com.demo:id/title" class="android.widget.TextView" text="Bluetooth" bounds="[50,220][500,330]" clickable="true" enabled="true" />
      <node index="1" resource-id="android:id/switch_widget" class="android.widget.Switch" bounds="[800,220][1030,330]" clickable="true" enabled="true" />
    </node>
  </node>
</hierarchy>`

	x := 200.0 / 1080.0
	y := 270.0 / 1920.0

	node, _ := FindBestNodeAt(overlappingXML, x, y, 1080, 1920)
	if node == nil {
		t.Fatal("expected to find a node")
	}

	// Should pick the clickable TextView, not the RecyclerView
	if node.ResourceID != "com.demo:id/title" {
		t.Errorf("expected TextView (com.demo:id/title), got %s (%s)", node.ResourceID, node.Class)
	}
}

func TestLocatorGeneration(t *testing.T) {
	// Login button has unique text "Login" and unique resourceId
	node := &XMLNode{
		ResourceID:  "com.demo:id/login",
		ContentDesc: "Submit Login",
		Text:        "Login",
		Class:       "android.widget.Button",
		Bounds:      "[300,800][780,900]",
		Clickable:   "true",
		Enabled:     "true",
	}

	locators := GenerateLocators(mockUIDump, node, 0.5, 0.45)

	if len(locators) < 3 {
		t.Fatalf("expected at least 3 locators (resourceId, text, contentDesc, coords), got %d", len(locators))
	}

	// Highest confidence should be resourceId=100 (unique)
	top := locators[0]
	if top.Strategy != "resourceId" || top.Confidence != 100 {
		t.Errorf("expected top locator to be unique resourceId with confidence=100, got strategy=%s confidence=%d", top.Strategy, top.Confidence)
	}

	// Coordinates should be last
	last := locators[len(locators)-1]
	if last.Strategy != "coordinates" {
		t.Errorf("expected last locator to be coordinates, got %s", last.Strategy)
	}
}

func TestAnchorExtraction(t *testing.T) {
	// Switch next to "Bluetooth" text
	xmlWithSiblings := `<?xml version="1.0" encoding="utf-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.LinearLayout" bounds="[0,0][1080,100]" enabled="true">
    <node index="0" class="android.widget.TextView" text="Bluetooth" bounds="[50,10][500,90]" />
    <node index="1" resource-id="android:id/switch_widget" class="android.widget.Switch" bounds="[800,10][1030,90]" clickable="true" enabled="true" />
  </node>
</hierarchy>`

	target := &XMLNode{
		ResourceID: "android:id/switch_widget",
		Class:      "android.widget.Switch",
		Bounds:     "[800,10][1030,90]",
		Clickable:  "true",
		Enabled:    "true",
	}

	anchor := ExtractAnchor(xmlWithSiblings, target)
	if anchor == nil {
		t.Fatal("expected anchor to be extracted")
	}

	if anchor.SiblingText != "Bluetooth" {
		t.Errorf("expected siblingText='Bluetooth', got '%s'", anchor.SiblingText)
	}
	if anchor.ParentClass != "android.widget.LinearLayout" {
		t.Errorf("expected parentClass='android.widget.LinearLayout', got '%s'", anchor.ParentClass)
	}
}

func TestCompilerPipeline(t *testing.T) {
	// Simulate raw events
	events := []RawEvent{
		{Type: "launch", Package: "com.demo"},
		{
			Type:         "click",
			TouchX:       0.5,
			TouchY:       850.0 / 1920.0,
			ScreenWidth:  1080,
			ScreenHeight: 1920,
			UIXML:        mockUIDump,
		},
		{Type: "input", Text: "h"},
		{Type: "input", Text: "i"},
	}

	script := CompileScript(events)

	// Should have 7 steps: launch, wait, assert, click (with locators), merged input "hi", terminate, wait-for-close
	if len(script.Steps) != 7 {
		t.Fatalf("expected 7 steps, got %d", len(script.Steps))
	}

	// Launch step
	if script.Steps[0].Launch == nil || script.Steps[0].Launch.Package != "com.demo" {
		t.Error("expected launch step for com.demo")
	}

	// Wait step
	if script.Steps[1].Wait == nil || script.Steps[1].Wait.Class != "android.widget.TextView" {
		t.Error("expected wait step for android.widget.TextView after launch")
	}

	// Assert step
	if script.Steps[2].Assert == nil || script.Steps[2].Assert.Text != "Demo" {
		t.Errorf("expected assert step for package 'com.demo' -> text 'Demo', got %v", script.Steps[2].Assert)
	}

	// Click step should have locators
	click := script.Steps[3].Click
	if click == nil {
		t.Fatal("expected click step")
	}
	if len(click.Locators) == 0 {
		t.Fatal("expected locators array to be populated")
	}

	// Top locator should be resourceId or text (both unique for Login button)
	topLoc := click.Locators[0]
	if topLoc.Confidence < 90 {
		t.Errorf("expected high confidence top locator, got strategy=%s confidence=%d", topLoc.Strategy, topLoc.Confidence)
	}

	// Input step should be merged "hi"
	if script.Steps[4].Input == nil || script.Steps[4].Input.Text != "hi" {
		t.Errorf("expected merged input 'hi', got %v", script.Steps[4].Input)
	}

	// Terminate step should be appended at the end
	if script.Steps[5].Terminate == nil || script.Steps[5].Terminate.Package != "com.demo" {
		t.Errorf("expected terminate step for package 'com.demo', got %v", script.Steps[5].Terminate)
	}

	// Wait-for-close step should be appended after terminate
	if script.Steps[6].Wait == nil || script.Steps[6].Wait.Condition != "hidden" {
		t.Errorf("expected wait-for-close step, got %v", script.Steps[6].Wait)
	}
}

func TestLocatorEngine(t *testing.T) {
	// Test Resource ID mapping (Username field: bounds [100,500][980,600])
	x, y, err := FindElement(mockUIDump, ElementQuery{ResourceID: "com.demo:id/username"}, 1080, 1920)
	if err != nil {
		t.Fatalf("failed to locate by ResourceID: %v", err)
	}
	if x != 0.5 || y != 550.0/1920.0 {
		t.Errorf("incorrect coordinate mapping for username field. Got (%f, %f)", x, y)
	}

	// Test Content Description mapping
	x, y, err = FindElement(mockUIDump, ElementQuery{ContentDesc: "Submit Login"}, 1080, 1920)
	if err != nil {
		t.Fatalf("failed to locate by ContentDesc: %v", err)
	}
	if x != 0.5 || y != 850.0/1920.0 {
		t.Errorf("incorrect coordinate mapping for Login button via ContentDesc. Got (%f, %f)", x, y)
	}

	// Test Text match mapping
	x, y, err = FindElement(mockUIDump, ElementQuery{Text: "Login"}, 1080, 1920)
	if err != nil {
		t.Fatalf("failed to locate by Text: %v", err)
	}
	if x != 0.5 || y != 850.0/1920.0 {
		t.Errorf("incorrect coordinate mapping for Login button via Text. Got (%f, %f)", x, y)
	}

	// Test missing element
	_, _, err = FindElement(mockUIDump, ElementQuery{ResourceID: "nonexistent"}, 1080, 1920)
	if err == nil {
		t.Error("expected error for nonexistent element query, got nil")
	}
}

func TestDSLParser(t *testing.T) {
	yamlScript := `
steps:
  - launch:
      package: com.demo
  - click:
      resourceId: com.demo:id/username
  - wait:
      resourceId: com.demo:id/login
      condition: visible
      timeoutMs: 4000
  - assert:
      resourceId: com.demo:id/username
      condition: equals
      value: admin-user
`
	script, err := ParseScript(strings.NewReader(yamlScript))
	if err != nil {
		t.Fatalf("failed to parse YAML DSL: %v", err)
	}

	if len(script.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(script.Steps))
	}

	if script.Steps[2].Wait.ResourceID != "com.demo:id/login" || script.Steps[2].Wait.Condition != "visible" || script.Steps[2].Wait.TimeoutMs != 4000 {
		t.Errorf("Wait step parsing mismatch: %+v", script.Steps[2].Wait)
	}

	if script.Steps[3].Assert.ResourceID != "com.demo:id/username" || script.Steps[3].Assert.Condition != "equals" || script.Steps[3].Assert.Value != "admin-user" {
		t.Errorf("Assert step parsing mismatch: %+v", script.Steps[3].Assert)
	}
}

func TestDSLParserWithLocators(t *testing.T) {
	yamlScript := `
steps:
  - click:
      locators:
        - strategy: resourceId
          value: com.demo:id/login
          confidence: 100
        - strategy: text
          value: Login
          confidence: 95
        - strategy: coordinates
          confidence: 10
          x: 0.5
          y: 0.45
      anchor:
        siblingText: "Submit"
        parentClass: "android.widget.LinearLayout"
`
	script, err := ParseScript(strings.NewReader(yamlScript))
	if err != nil {
		t.Fatalf("failed to parse YAML with locators: %v", err)
	}

	click := script.Steps[0].Click
	if len(click.Locators) != 3 {
		t.Fatalf("expected 3 locators, got %d", len(click.Locators))
	}
	if click.Locators[0].Strategy != "resourceId" || click.Locators[0].Confidence != 100 {
		t.Errorf("first locator mismatch: %+v", click.Locators[0])
	}
	if click.Anchor == nil || click.Anchor.SiblingText != "Submit" {
		t.Errorf("anchor mismatch: %+v", click.Anchor)
	}
}

func TestExecutionRunner(t *testing.T) {
	driver := &mockDriver{}
	runner := NewRunner(driver)

	yamlScript := `
steps:
  - launch:
      package: com.demo
  - click:
      resourceId: com.demo:id/username
  - input:
      text: admin
  - click:
      x: 0.5
      y: 0.25
`
	script, err := ParseScript(strings.NewReader(yamlScript))
	if err != nil {
		t.Fatalf("failed to parse YAML DSL: %v", err)
	}

	ctx := context.Background()
	report, err := runner.Run(ctx, script)
	if err != nil {
		t.Fatalf("runner execution error: %v", err)
	}

	if !report.Success {
		t.Errorf("runner report failed. Results: %+v", report.Results)
	}

	if report.PassedSteps != 4 || report.TotalSteps != 4 {
		t.Errorf("step completion numbers mismatch. Passed %d, Total %d", report.PassedSteps, report.TotalSteps)
	}
}

func TestExecutionWithLocators(t *testing.T) {
	driver := &mockDriver{}
	runner := NewRunner(driver)

	// Test that the runner correctly processes locators array
	x, y := 0.5, 850.0/1920.0
	script := &Script{
		Steps: []Step{
			{
				Click: &ClickParams{
					Locators: []Locator{
						{Strategy: "text", Value: "Login", Confidence: 95},
						{Strategy: "resourceId", Value: "com.demo:id/login", Confidence: 100},
						{Strategy: "coordinates", Confidence: 10, X: x, Y: y},
					},
				},
			},
		},
	}

	report, err := runner.Run(context.Background(), script)
	if err != nil {
		t.Fatalf("runner execution error: %v", err)
	}
	if !report.Success {
		t.Errorf("expected success, got: %+v", report.Results)
	}
	if len(driver.tappedCoords) != 1 {
		t.Errorf("expected exactly 1 tap, got %d", len(driver.tappedCoords))
	}
}

func TestWaitAndAssertions(t *testing.T) {
	driver := &mockDriver{}
	runner := NewRunner(driver)

	var dumpCount int
	driver.dumpUIFn = func() string {
		dumpCount++
		if dumpCount < 2 {
			return `<?xml version="1.0" encoding="utf-8"?><hierarchy></hierarchy>`
		}
		return mockUIDump
	}

	yamlScript := `
steps:
  - wait:
      resourceId: com.demo:id/login
      condition: visible
      timeoutMs: 2000
  - assert:
      resourceId: com.demo:id/username
      condition: equals
      value: admin-user
  - assert:
      resourceId: com.demo:id/username
      condition: contains
      value: admin
`
	script, err := ParseScript(strings.NewReader(yamlScript))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	ctx := context.Background()
	report, err := runner.Run(ctx, script)
	if err != nil {
		t.Fatalf("runner execution failed: %v", err)
	}

	if !report.Success {
		t.Errorf("expected assertions to pass, report results: %+v", report.Results)
	}

	if report.PassedSteps != 3 {
		t.Errorf("expected 3 steps to pass, got %d", report.PassedSteps)
	}
}

func TestStepRetries(t *testing.T) {
	driver := &mockDriver{}
	runner := NewRunner(driver)

	var tapCount int
	driver.tapFn = func(x, y float64) error {
		tapCount++
		if tapCount < 2 {
			return errors.New("touch screen hardware timeout")
		}
		return nil
	}

	yamlScript := `
steps:
  - click:
      x: 0.5
      y: 0.5
`
	script, _ := ParseScript(strings.NewReader(yamlScript))
	ctx := context.Background()
	report, err := runner.Run(ctx, script)
	if err != nil {
		t.Fatalf("runner execution error: %v", err)
	}

	if !report.Success {
		t.Errorf("expected step retry to self-heal and succeed, got results: %+v", report.Results)
	}

	if tapCount != 2 {
		t.Errorf("expected Tap to be called exactly twice, got %d", tapCount)
	}
}

func TestRecorderAndCompiler(t *testing.T) {
	mgr := NewRecorderManager()
	serial := "TEST_SERIAL"
	driver := &mockDriver{}

	mgr.StartRecording(serial, "")
	if !mgr.IsRecording(serial) {
		t.Error("expected recording session to be active")
	}

	ctx := context.Background()

	// Record two clicks at same spot (should be deduped by compiler)
	err := mgr.RecordClick(ctx, serial, driver, 0.5, 850.0/1920.0)
	if err != nil {
		t.Fatalf("failed to record click: %v", err)
	}
	err = mgr.RecordClick(ctx, serial, driver, 0.5, 850.0/1920.0)
	if err != nil {
		t.Fatalf("failed to record duplicate click: %v", err)
	}

	err = mgr.RecordTextInput(serial, "user text input")
	if err != nil {
		t.Fatalf("failed to record text input: %v", err)
	}

	// Stop recording returns raw events
	rawEvents, err := mgr.StopRecording(serial)
	if err != nil {
		t.Fatalf("failed to stop recording: %v", err)
	}

	if len(rawEvents) != 3 { // 2 clicks + 1 input (no dedup at recorder level)
		t.Fatalf("expected 3 raw events, got %d", len(rawEvents))
	}

	// Compile the raw events
	script := CompileScript(rawEvents)

	// After compilation: 1 click (duplicate filtered) + 1 input = 2 steps
	if len(script.Steps) != 2 {
		t.Fatalf("expected 2 compiled steps (duplicate click filtered), got %d", len(script.Steps))
	}

	// Click step should have locators
	clickStep := script.Steps[0].Click
	if clickStep == nil {
		t.Fatal("expected click step")
	}
	if len(clickStep.Locators) == 0 {
		t.Fatal("expected locators to be populated")
	}

	// Should have found the Login button (unique resourceId and text)
	hasResourceId := false
	hasText := false
	for _, loc := range clickStep.Locators {
		if loc.Strategy == "resourceId" && loc.Value == "com.demo:id/login" {
			hasResourceId = true
		}
		if loc.Strategy == "text" && loc.Value == "Login" {
			hasText = true
		}
	}
	if !hasResourceId {
		t.Error("expected resourceId locator for com.demo:id/login")
	}
	if !hasText {
		t.Error("expected text locator for 'Login'")
	}

	// Text input step
	inputStep := script.Steps[1].Input
	if inputStep.Text != "user text input" {
		t.Errorf("recorded text input mismatch. Got %s, want 'user text input'", inputStep.Text)
	}
}

func TestRunnerVariablesAndConditions(t *testing.T) {
	driver := &mockDriver{
		dumpUIFn: func() string {
			return mockUIDump
		},
	}
	runner := NewRunner(driver)

	script := &Script{
		Variables: map[string]string{
			"usernameVar": "secretAdmin",
		},
		Steps: []Step{
			// 1. Variable Input Step
			{
				Input: &InputParams{
					Variable:   "usernameVar",
					ResourceID: "com.demo:id/username",
				},
			},
			// 2. Condition Step (Matches: Exists -> Then block runs)
			{
				If: &IfCondition{
					Exists: &ExistsCondition{
						ResourceID: "com.demo:id/login",
					},
				},
				Then: []Step{
					{
						Click: &ClickParams{
							ResourceID: "com.demo:id/login",
						},
					},
				},
				Else: []Step{
					{
						Click: &ClickParams{
							ResourceID: "com.demo:id/username",
						},
					},
				},
			},
			// 3. Condition Step (Mismatches: Exists -> Else block runs)
			{
				If: &IfCondition{
					Exists: &ExistsCondition{
						ResourceID: "com.demo:id/nonexistent",
					},
				},
				Then: []Step{
					{
						Click: &ClickParams{
							ResourceID: "com.demo:id/login",
						},
					},
				},
				Else: []Step{
					{
						Click: &ClickParams{
							ResourceID: "com.demo:id/password",
						},
					},
				},
			},
		},
	}

	report, err := runner.Run(context.Background(), script)
	if err != nil {
		t.Fatalf("failed to run script: %v", err)
	}

	if !report.Success {
		t.Fatalf("expected script run success, got report error: %v", report.Results)
	}

	// Verify inputs: Variable "usernameVar" resolved to "secretAdmin"
	if len(driver.inputsReceived) != 1 || driver.inputsReceived[0] != "secretAdmin" {
		t.Errorf("variable resolution failed: inputs received = %v", driver.inputsReceived)
	}

	// Verify clicks:
	// - 1 tap from handleInput (focus click because target node is not focused)
	// - 1 tap from Step 2 (Then block -> Click com.demo:id/login)
	// - 1 tap from Step 3 (Else block -> Click com.demo:id/password)
	if len(driver.tappedCoords) != 3 {
		t.Errorf("expected 3 click events (including focus tap), got %d", len(driver.tappedCoords))
	}
}

func TestUIElementTreeAndResolve(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="utf-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,1920]">
    <node index="0" class="android.widget.LinearLayout" bounds="[0,0][1080,1920]">
      <node index="0" resource-id="com.demo:id/container" class="android.widget.RelativeLayout" bounds="[50,100][1030,500]">
        <node index="0" resource-id="com.demo:id/label" class="android.widget.TextView" text="First Name" bounds="[100,150][500,250]" />
        <node index="1" resource-id="com.demo:id/input" class="android.widget.EditText" bounds="[500,150][980,250]" clickable="true" />
      </node>
    </node>
  </node>
</hierarchy>`

	root, err := ParseXMLTree(xmlData)
	if err != nil {
		t.Fatalf("failed to parse UI tree: %v", err)
	}

	if root.Class != "android.widget.FrameLayout" {
		t.Errorf("expected root class FrameLayout, got %s", root.Class)
	}

	elements := root.FlattenTree()
	if len(elements) != 5 {
		t.Errorf("expected 5 elements in tree, got %d", len(elements))
	}

	// Test ResolveElement with exact match
	target := &UIElement{
		ResourceID: "com.demo:id/input",
		Class:      "android.widget.EditText",
	}

	match, score, err := ResolveElement(root, target, nil)
	if err != nil {
		t.Fatalf("failed to resolve element: %v", err)
	}

	if match.ResourceID != "com.demo:id/input" {
		t.Errorf("expected resolved ResourceID com.demo:id/input, got %s", match.ResourceID)
	}
	if score < 120 { // 100 (resource id) + 20 (class)
		t.Errorf("expected high score, got %d", score)
	}

	// Test sibling text matching
	targetWithSibling := &UIElement{
		Class: "android.widget.EditText",
		Parent: &UIElement{
			Class: "android.widget.RelativeLayout",
			Children: []*UIElement{
				{Text: "First Name"},
			},
		},
	}

	siblingMatch, siblingScore, err := ResolveElement(root, targetWithSibling, nil)
	if err != nil {
		t.Fatalf("failed to resolve with sibling: %v", err)
	}
	if siblingMatch.ResourceID != "com.demo:id/input" {
		t.Errorf("expected sibling match on input field, got %s", siblingMatch.ResourceID)
	}
	if siblingScore < 50 { // sibling bonus is +50
		t.Errorf("expected score with sibling bonus, got %d", siblingScore)
	}
}

func TestHitTestingUpwardSearch(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="utf-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,1920]">
    <node index="0" resource-id="com.demo:id/card" class="android.widget.LinearLayout" bounds="[100,200][980,500]" clickable="true" enabled="true">
      <node index="0" class="android.widget.TextView" text="Card Title" bounds="[150,220][500,320]" />
      <node index="1" class="android.widget.TextView" text="Card Description" bounds="[150,340][900,480]" />
    </node>
  </node>
</hierarchy>`

	root, err := ParseXMLTree(xmlData)
	if err != nil {
		t.Fatalf("failed to parse UI tree: %v", err)
	}

	// User clicks on "Card Title" which is not clickable itself, but is inside clickable "card"
	bestEl := FindBestElementAt(root, 300.0/1080.0, 270.0/1920.0, 1080, 1920)
	if bestEl == nil {
		t.Fatal("expected to find element")
	}

	if bestEl.ResourceID != "com.demo:id/card" {
		t.Errorf("expected click to bubble up to clickable container 'card', got %s (%s)", bestEl.ResourceID, bestEl.Class)
	}
}

func TestResolveElement_WithAnchorDisambiguation(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="utf-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,1920]">
    <node index="0" resource-id="com.demo:id/recycler_view" class="androidx.recyclerview.widget.RecyclerView" bounds="[0,200][1080,1920]">
      <node index="0" class="android.widget.LinearLayout" bounds="[0,200][1080,350]">
        <node index="0" class="android.widget.TextView" text="Flexible windows" bounds="[50,220][500,330]" />
      </node>
      <node index="1" class="android.widget.LinearLayout" bounds="[0,360][1080,510]">
        <node index="0" class="android.widget.TextView" text="Mobile network" bounds="[50,380][500,490]" />
      </node>
    </node>
  </node>
</hierarchy>`

	root, err := ParseXMLTree(xmlData)
	if err != nil {
		t.Fatalf("failed to parse UI tree: %v", err)
	}

	target := &UIElement{
		Class: "android.widget.LinearLayout",
	}

	anchor := &AnchorContext{
		SiblingText: "Flexible windows",
		ParentClass: "androidx.recyclerview.widget.RecyclerView",
	}

	match, score, err := ResolveElement(root, target, anchor)
	if err != nil {
		t.Fatalf("failed to resolve: %v", err)
	}

	// Verify that the matched element has bounds for index 0 (top = 200) rather than index 1 (top = 360)
	if match.Bounds.Top != 200 {
		t.Errorf("expected matched element to be the first LinearLayout (top=200), got top=%d (score=%d)", match.Bounds.Top, score)
	}
}

func TestShouldTapOriginalCoords(t *testing.T) {
	runner := &Runner{}

	node := &UIElement{
		Class: "androidx.recyclerview.widget.RecyclerView",
		Bounds: Rect{Left: 0, Top: 110, Right: 1080, Bottom: 1920},
	}

	tx := 0.898413
	ty := 0.092232
	// coordinates in pixels:
	// tx_pixel = 0.898413 * 1080 = 970
	// ty_pixel = 0.092232 * 1920 = 177

	shouldTap := runner.shouldTapOriginalCoords(node, &tx, &ty, 1080, 1920)
	if !shouldTap {
		t.Error("expected shouldTapOriginalCoords to return true for RecyclerView containing touch coords")
	}

	// Case 2: small TextView
	smallNode := &UIElement{
		Class: "android.widget.TextView",
		Bounds: Rect{Left: 100, Top: 150, Right: 200, Bottom: 200},
	}
	shouldTapSmall := runner.shouldTapOriginalCoords(smallNode, &tx, &ty, 1080, 1920)
	if shouldTapSmall {
		t.Error("expected shouldTapOriginalCoords to return false for small TextView")
	}
}

func TestAutoScrollFallback(t *testing.T) {
	xmlWithoutTarget := `<?xml version="1.0" encoding="utf-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,1920]">
    <node index="0" resource-id="com.demo:id/recycler_view" class="androidx.recyclerview.widget.RecyclerView" bounds="[0,200][1080,1920]" scrollable="true">
      <node index="0" class="android.widget.LinearLayout" bounds="[0,200][1080,350]">
        <node index="0" class="android.widget.TextView" text="Item 1" bounds="[50,220][500,330]" />
      </node>
    </node>
  </node>
</hierarchy>`

	xmlWithTarget := `<?xml version="1.0" encoding="utf-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,1920]">
    <node index="0" resource-id="com.demo:id/recycler_view" class="androidx.recyclerview.widget.RecyclerView" bounds="[0,200][1080,1920]" scrollable="true">
      <node index="0" class="android.widget.LinearLayout" bounds="[0,200][1080,350]">
        <node index="0" class="android.widget.TextView" text="Item 1" bounds="[50,220][500,330]" />
      </node>
      <node index="1" resource-id="com.demo:id/target_item" class="android.widget.LinearLayout" bounds="[0,360][1080,510]" clickable="true">
        <node index="0" class="android.widget.TextView" text="Special Target Item" bounds="[50,380][500,490]" />
      </node>
    </node>
  </node>
</hierarchy>`

	dumpCount := 0
	driver := &mockDriver{
		dumpUIFn: func() string {
			dumpCount++
			if dumpCount == 1 {
				return xmlWithoutTarget
			}
			return xmlWithTarget
		},
	}

	runner := NewRunner(driver)
	params := &ClickParams{
		Target: &UIElement{
			ResourceID: "com.demo:id/target_item",
			Class:      "android.widget.LinearLayout",
		},
	}

	ctx := context.Background()
	err := runner.handleClickWithTarget(ctx, params)
	if err != nil {
		t.Fatalf("handleClickWithTarget failed: %v", err)
	}

	// Verify that a swipe gesture was registered (scrollCount = 1)
	if len(driver.swipesReceived) != 1 {
		t.Errorf("expected 1 swipe gesture, got %d", len(driver.swipesReceived))
	}

	// Verify that target was tapped (top = 360, bottom = 510, centerX = 540, centerY = 435)
	if len(driver.tappedCoords) != 1 {
		t.Fatalf("expected 1 tap coords, got %d", len(driver.tappedCoords))
	}
	expectedX := 540.0 / 1080.0
	expectedY := 435.0 / 1920.0
	if math.Abs(driver.tappedCoords[0][0]-expectedX) > 0.01 || math.Abs(driver.tappedCoords[0][1]-expectedY) > 0.01 {
		t.Errorf("expected tap at (%f, %f), got (%f, %f)", expectedX, expectedY, driver.tappedCoords[0][0], driver.tappedCoords[0][1])
	}
}

func TestParseXMLTree_MultipleTopLevelNodes(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="utf-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" bounds="[0,0][1080,100]">
    <node index="0" class="android.widget.TextView" text="Status Bar" bounds="[0,0][1080,100]" />
  </node>
  <node index="1" class="android.widget.FrameLayout" bounds="[0,100][1080,1920]">
    <node index="0" class="android.widget.TextView" text="Main Window" bounds="[0,100][1080,1920]" />
  </node>
</hierarchy>`

	root, err := ParseXMLTree(xmlData)
	if err != nil {
		t.Fatalf("failed to parse multiple top-level nodes: %v", err)
	}

	// Should wrap in a virtual root
	if root.Class != "hierarchy" {
		t.Errorf("expected virtual root class 'hierarchy', got %q", root.Class)
	}

	// Should contain 2 children
	if len(root.Children) != 2 {
		t.Fatalf("expected 2 child elements under virtual root, got %d", len(root.Children))
	}

	// Union bounds should be [0,0][1080,1920]
	if root.Bounds.Left != 0 || root.Bounds.Top != 0 || root.Bounds.Right != 1080 || root.Bounds.Bottom != 1920 {
		t.Errorf("incorrect virtual bounds: %+v", root.Bounds)
	}
}
