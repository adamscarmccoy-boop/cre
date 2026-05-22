package tests

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prequel-dev/cre/pkg/logs"
	"github.com/prequel-dev/cre/pkg/ruler"
	"github.com/prequel-dev/prequel-compiler/pkg/ast"
	"github.com/prequel-dev/prequel-compiler/pkg/compiler"
	"github.com/prequel-dev/prequel-logmatch/pkg/entry"
	"github.com/prequel-dev/prequel-logmatch/pkg/format"
	lm "github.com/prequel-dev/prequel-logmatch/pkg/match"
	"github.com/prequel-dev/prequel-logmatch/pkg/scanner"
	"github.com/prequel-dev/prequel-logmatch/pkg/timez"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

const (
	creFolders    = "cre-*"
	creRules      = "*.yaml"
	testLogFile   = "test.log"
	testFPLogFile = "test-fp.log"

	futureMark = int64(math.MaxInt64) - 1 // Avoid future issues with MaxInt64 used as a flag
)

var (
	rulesPath        = os.Getenv("RULES_PATH")
	level            = os.Getenv("LOG_LEVEL")
	defaultRulesPath = "../rules"
	defaultLogLevel  = "info"
)

func initLogger() {
	logs.InitLogger(logs.WithPretty(), logs.WithLevel(strings.ToUpper(level)))
}

func TestMain(m *testing.M) {
	initLogger()

	if rulesPath == "" {
		rulesPath = defaultRulesPath
	}

	if level == "" {
		level = defaultLogLevel
	}

	log.Info().Str("rulesPath", rulesPath).Msg("Starting tests")
	code := m.Run()
	os.Exit(code)
}

func TestCres(t *testing.T) {

	// Find all CRE directories
	cres, err := filepath.Glob(filepath.Join(rulesPath, creFolders))
	if err != nil {
		t.Fatalf("Error finding CRE test files: %v", err)
	}

	// Read each CRE directory and run the tests
	for _, cre := range cres {

		var (
			ruleData []byte
			err      error
		)

		log.Info().Str("cre", cre).Msg("Reading CRE directory")

		rules, err := filepath.Glob(filepath.Join(cre, creRules))
		if err != nil {
			t.Fatalf("Error finding CRE rule files: %v", err)
		}

		if len(rules) != 1 {
			t.Fatalf("Expected 1 rule file, got %d", len(rules))
		}

		ruleData, err = os.ReadFile(rules[0])
		if err != nil {
			t.Fatalf("Error reading CRE rule file %s: %v", rules[0], err)
		}

		hashedRules, err := ruler.HashRules(ruleData)
		if err != nil {
			t.Fatalf("Fail hash rules for file %s: %v", rules[0], err)
		}

		if len(hashedRules) != 1 {
			t.Fatalf("Expected 1 rule after hashing, got %d", len(hashedRules))
		}

		var r = hashedRules[0]

		// Remarsh so we can hand it do the parser.
		nData, err := yaml.Marshal(r)
		if err != nil {
			t.Fatalf("Error marshalling hashed rule: %v", err)
		}

		testData, err := os.ReadFile(filepath.Join(cre, testLogFile))
		if err != nil {
			t.Fatalf("Error reading CRE test file: %v", err)
		}

		// Optional FP log file
		testFpData, _ := os.ReadFile(filepath.Join(cre, testFPLogFile))

		t.Run(filepath.Base(cre), func(t *testing.T) {
			hits, err := _eval([]byte(testData), nData)
			if err != nil {
				t.Fatalf("Error running detection: %v", err)
			}

			if len(hits) == 0 {
				t.Fatalf("Expected at least one problem")
			}

			if len(testFpData) > 0 {
				hits, err := _eval([]byte(testFpData), nData)

				if err != nil {
					t.Fatalf("Error running detection: %v", err)
				}

				if len(hits) != 0 {
					t.Fatalf("Expected no problems, got %d", len(hits))
				}

			}
		})
	}
}

type stubRuntime struct {
	matches []lm.Hits
}

func (r *stubRuntime) NewCbMatch(params compiler.MatchParamsT) compiler.CallbackT {
	return func(ctx context.Context, param any) error {
		m, ok := param.(lm.Hits)
		if !ok {
			return errors.New("unexpected parameter type for match callback")
		}

		r.matches = append(r.matches, m)
		return nil
	}
}

func (r *stubRuntime) NewCbAssert(params compiler.AssertParamsT) compiler.CallbackT {
	return func(ctx context.Context, param any) error {
		return nil
	}
}

func _eval(data, ruleData []byte) ([]lm.Hits, error) {
	ctx := context.Background()
	stub := &stubRuntime{}

	opts := []compiler.CompilerOptT{
		compiler.WithRuntime(stub),
	}

	objs, err := compiler.Compile(ruleData, ast.AstScopeNode, opts...)

	if err != nil {
		return nil, fmt.Errorf("error compiling rules: %v", err)
	}

	if len(objs) != 1 {
		return nil, fmt.Errorf("expected exactly one compiled object, got %d", len(objs))
	}

	mm, ok := objs[0].Object.(lm.Matcher)
	if !ok {
		return nil, fmt.Errorf("unexpected type for compiled object: %T", objs[0])
	}

	parser, err := _discover(data)
	if err != nil {
		return nil, fmt.Errorf("error discovering log format: %v", err)
	}

	var scanLine lm.ScanLine
	scanF := func(entry entry.LogEntry) bool {
		scanLine.Reset(entry)
		hits := mm.Scan(&scanLine)
		if hits.Cnt > 0 {
			objs[0].Cb(ctx, hits)
		}
		return false
	}

	err = scanner.ScanForward(
		bytes.NewReader(data),
		parser.ReadEntry,
		scanF,
		scanner.WithFold(true), // Fold is required to handle multi-line log entries
	)

	if err != nil {
		return nil, err
	}

	// Final flush to catch any negative matchers
	hits := mm.Eval(futureMark)
	if hits.Cnt > 0 {
		objs[0].Cb(ctx, hits)
	}

	return stub.matches, nil
}

func _discover(data []byte) (format.ParserI, error) {

	// Detect format
	if factory, _, err := format.Detect(bytes.NewReader(data)); err == nil {
		return factory.New(), nil
	}

	// Fallback to timestamp parsing if format detection fails, which is a common
	// failure mode for CRE logs that have a lot of variability in their structure but consistent timestamps.
	factory, _, err := timez.DetectFormat(bytes.NewReader(data))

	switch {
	case err != nil:
		return nil, err
	case factory == nil:
		return nil, errors.New("unable to detect log format")
	}

	return factory.New(), nil
}
