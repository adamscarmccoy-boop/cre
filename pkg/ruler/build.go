package ruler

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/prequel-dev/prequel-compiler/pkg/ast"
	"github.com/prequel-dev/prequel-compiler/pkg/compiler"

	"github.com/rs/zerolog/log"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

var (
	ErrInvalidType     = errors.New("invalid type")
	ErrDuplicateRuleId = errors.New("duplicate rule id")
)

var (
	packageName = "cre-rules"
	tagsDir     = "tags"
	tagsYaml    = "tags.yaml"
	catsYaml    = "categories.yaml"
)

func RunBuild(inPath, outPath, vers string, exclude []string) error {

	if outPath == "" {
		var err error
		outPath, err = os.Getwd()
		if err != nil {
			log.Error().Err(err).Msg("Fail os.Getwd()")
			return err
		}
	}

	if vers == "" {
		vers = Semver()
	}

	if !strings.HasPrefix(vers, "v") {
		vers = "v" + vers
	}

	if !semver.IsValid(vers) {
		return fmt.Errorf("invalid semver: %s", vers)
	}

	if outPath != "" {
		if err := os.MkdirAll(outPath, 0755); err != nil {
			log.Error().Err(err).Msg("Fail mkdir all")
			return err
		}
	}

	if err := _build(vers, inPath, outPath, packageName, exclude); err != nil {
		return err
	}

	return nil
}

type tagDataT struct {
	dupes tagsT
	tSec  RuleIncludeT
	cSec  RuleIncludeT
}

func processTags(inPath string) (*tagDataT, error) {
	var (
		tagsData       []byte
		categoriesData []byte
		td             = &tagDataT{
			dupes: make(tagsT),
		}
		err error
	)

	tagsData, err = os.ReadFile(filepath.Join(inPath, tagsDir, tagsYaml))
	if err != nil {
		log.Error().Err(err).Msg("Fail read tags")
		return nil, err
	}

	if err := yaml.Unmarshal(tagsData, &td.tSec); err != nil {
		log.Error().Err(err).Msg("Fail unmarshal tags")
		return nil, err
	}

	categoriesData, err = os.ReadFile(filepath.Join(inPath, tagsDir, catsYaml))
	if err != nil {
		log.Error().Err(err).Msg("Fail read categories")
		return nil, err
	}

	if err := yaml.Unmarshal(categoriesData, &td.cSec); err != nil {
		log.Error().Err(err).Msg("Fail unmarshal categories")
		return nil, err
	}

	if err := validateTagsFields(td.tSec, td.dupes); err != nil {
		log.Error().Err(err).Str("file", filepath.Join(inPath, tagsDir, tagsYaml)).Msg("Fail validate tags")
		return nil, err
	}

	if err := validateCategoriesFields(td.cSec, td.dupes); err != nil {
		log.Error().Err(err).Str("file", filepath.Join(inPath, tagsDir, catsYaml)).Msg("Fail validate categories")
		return nil, err
	}

	return td, nil
}

func processRules(path string, ruleDupes dupesT, tags tagsT, opts ...ast.ParseOpt) ([]ruleDataT, error) {
	var (
		allRules []ruleDataT
	)

	yamls, err := os.ReadDir(path)
	if err != nil {
		log.Error().Err(err).Msg("Fail read rules")
		return nil, err
	}

	for _, y := range yamls {

		log.Debug().
			Str("file", y.Name()).
			Msg("Processing rule")

		if !strings.HasSuffix(y.Name(), ".yaml") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(path, y.Name()))
		if err != nil {
			log.Error().Err(err).Msg("Fail read rules")
			return nil, err
		}

		opts := append(opts,
			ast.WithStrict(true),
			ast.WithJQValidator(func(query string) error {
				_, err := gojq.Parse(query)
				return err
			}))

		rules, err := processRuleData(data, opts...)
		if err != nil {
			log.Error().Err(err).Msg("Fail process rule data")
			return nil, err
		}

		if err = validateRules(rules, ruleDupes, tags); err != nil {
			log.Error().Err(err).Str("file", filepath.Join(path, y.Name())).Msg("Fail validate rules")
			return nil, err
		}

		for _, r := range rules {

			log.Info().
				Str("hash", r.rule.Metadata.Hash).
				Str("id", r.rule.Cre.Id).
				Msg("Rule")

			allRules = append(allRules, r)
		}
	}

	return allRules, nil
}

type ruleDataT struct {
	dom     map[string]any
	rule    ast.AstRuleT
	payload []byte
}

func processRuleData(data []byte, opts ...ast.ParseOpt) ([]ruleDataT, error) {

	hashedRules, err := HashRules(data)
	if err != nil {
		log.Error().Err(err).Msg("Fail hash rules")
		return nil, err
	}

	// Regenerate the yaml for each of the hashed payloads and parse into rules to be returned for downstream processing.

	var outRules []ruleDataT

	for _, hr := range hashedRules {

		payload, err := yaml.Marshal(hr)
		if err != nil {
			log.Error().Err(err).Msg("Fail marshal rule")
			return nil, err
		}

		rules, err := ast.ParseRules(payload, opts...)
		if err != nil {
			log.Error().Err(err).Msg("Fail parse rules")
			var pe ast.ParseError
			if errors.As(err, &pe) {
				fmt.Println(pe.Format(true, true))
			}
			return nil, err
		}

		if len(rules) != 1 {
			return nil, fmt.Errorf("unexpected number of rules parsed from payload: %d", len(rules))
		}

		ruleData := ruleDataT{
			dom:     hr,
			rule:    rules[0],
			payload: payload,
		}
		outRules = append(outRules, ruleData)
	}

	return outRules, nil
}

func containsAny[T comparable](a, b []T) bool {
	for _, v := range b {
		log.Debug().Any("v", v).Any("a", a).Msg("containsAny")
		if slices.Contains(a, v) {
			return true
		}
	}
	return false
}

func _build(vers, inPath, outPath, packageName string, exclude []string) error {

	var (
		allRules  = make(map[string]ruleDataT)
		ruleDupes = make(dupesT)
		td        *tagDataT
		err       error
	)

	log.Info().Str("vers", vers).Str("outPath", outPath).Msg("Building")

	if td, err = processTags(inPath); err != nil {
		return err
	}

	log.Debug().
		Int("tags", len(td.tSec.Tags)).
		Int("categories", len(td.cSec.Categories)).
		Msg("Tags")

	cres, err := os.ReadDir(inPath)
	if err != nil {
		log.Error().Err(err).Msg("Fail read rules dir")
		return err
	}

	for _, e := range cres {
		zlog := log.With().Str("file", e.Name()).Logger()

		if !e.IsDir() {
			zlog.Debug().Msg("Skipping")
			continue
		}

		if !strings.HasPrefix(e.Name(), "cre-") && !strings.HasPrefix(e.Name(), "prequel-") {
			zlog.Debug().Msg("Skipping")
			continue
		}

		zlog.Debug().Msg("Processing target")

		rules, err := processRules(filepath.Join(inPath, e.Name()), ruleDupes, td.dupes)
		if err != nil {
			zlog.Error().Err(err).Msg("Fail process rules")
			return err
		}

		for _, r := range rules {
			if containsAny(r.rule.Cre.Tags, exclude) {
				zlog.Info().
					Str("id", r.rule.Cre.Id).
					Msg("Skipping rule due to exclude tag match")
				continue
			}
			allRules[r.rule.Cre.Id] = r
		}
	}

	doc, err := generateDocument(allRules)
	if err != nil {
		return err
	}

	/// Validate final document compiles
	if err = compileCombinedDoc(doc); err != nil {
		log.Error().Err(err).Msg("Fail compile")
		return err
	}

	fileName := makeFilename(packageName, vers)
	fullPath := filepath.Join(outPath, fileName)

	if err = writeFile(fullPath, doc); err != nil {
		return err
	}

	fmt.Printf("Wrote file: %s\n", fileName)

	return nil
}

func compileCombinedDoc(data []byte) error {
	_, err := compiler.Compile(data, ast.AstScopeNode)
	return err
}

func writeFile(fn string, data []byte) error {
	fh, err := os.OpenFile(fn, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	if _, err = fh.Write(data); err != nil {
		fh.Close()
		return err
	}

	return fh.Close()
}

func makeFilename(name, vers string) string {
	name = strings.TrimSuffix(name, ".yaml")
	vers = strings.TrimPrefix(vers, "v")

	return fmt.Sprintf("%s.%s.yaml", name, vers)
}

// Convert to document per section
func generateDocument(rules map[string]ruleDataT) ([]byte, error) {

	// Gather keys to produce consistent order output
	ruleKeys := make([]string, 0, len(rules))
	for key := range rules {
		ruleKeys = append(ruleKeys, key)
	}
	sort.Strings(ruleKeys)

	// Merge together the rule dom's into a single document.
	// Ideallly this would be N separate yaml documents; however,
	// the old parser does not support this so we are stuck with this
	// until the new parser is widely deployed.

	ruleList := make([]any, 0, len(rules))

	for _, key := range ruleKeys {
		r := rules[key]
		seqNode, ok := r.dom[kwRules].([]any)
		if !ok {
			return nil, fmt.Errorf("expected sequence node, got %T", r.dom[kwRules])
		}
		if len(seqNode) != 1 {
			return nil, fmt.Errorf("expected single rule in sequence, got %d", len(seqNode))
		}

		ruleList = append(ruleList, seqNode[0])
	}

	root := map[string]any{
		kwRules: ruleList,
	}

	return yaml.Marshal(root)
}
