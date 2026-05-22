package ruler

import (
	"errors"

	"github.com/rs/zerolog/log"
)

var (
	ErrMissingId             = errors.New("missing id field")
	ErrEmptyId               = errors.New("empty id field")
	ErrMissingName           = errors.New("missing name field")
	ErrEmptyName             = errors.New("empty name field")
	ErrMissingDisplayName    = errors.New("missing display name field")
	ErrEmptyDisplayName      = errors.New("empty display name field")
	ErrMissingDescription    = errors.New("missing description field")
	ErrEmptyDescription      = errors.New("empty description field")
	ErrInvalidTagsKind       = errors.New("invalid tags kind")
	ErrInvalidCategoriesKind = errors.New("invalid categories kind")
	ErrDuplicateName         = errors.New("duplicate name")
	ErrUnknownTag            = errors.New("unknown tag")
	ErrUnknownCategory       = errors.New("unknown category")
	ErrMissingCategory       = errors.New("missing category field")
)

type dupesT map[string]struct{}
type tagsT dupesT

var (
	kindTags       = "tags"
	kindCategories = "categories"
)

func validateTagsFields(t RuleIncludeT, tags tagsT) error {

	if t.Metadata.Kind != kindTags {
		return ErrInvalidTagsKind
	}

	return validateTags(t.Tags, t.Metadata.Kind, tags)

}

func validateCategoriesFields(t RuleIncludeT, tags tagsT) error {
	if t.Metadata.Kind != kindCategories {
		return ErrInvalidCategoriesKind
	}

	return validateTags(t.Categories, t.Metadata.Kind, tags)
	return nil
}

func validateTags(tags []TagT, kind string, dupes tagsT) error {

	for _, t := range tags {
		if t.Name == "" {
			log.Error().
				Any("tag", t).
				Str("kind", kind).
				Msg("Missing name")
			return ErrMissingDisplayName
		}

		if _, ok := dupes[t.Name]; ok {
			log.Error().
				Str("name", t.Name).
				Str("kind", kind).
				Msg("Duplicate name")
			return ErrDuplicateName
		}

		dupes[t.Name] = struct{}{}

		if t.DisplayName == "" {
			log.Error().
				Str("tag", t.Name).
				Msg("Missing display name")
			return ErrMissingDisplayName
		}

		if t.Description == "" {
			log.Error().
				Str("tag", t.Name).
				Msg("Missing description")
			return ErrMissingDescription
		}
	}

	return nil
}

func validateRules(rules []ruleDataT, allRules dupesT, tags tagsT) error {

	for _, r := range rules {

		log.Debug().
			Str("id", r.rule.Cre.Id).
			Msg("Processing rule")

		if r.rule.Cre.Id == "" {
			log.Error().
				Any("rule", r.rule).
				Msg("Missing CRE id")
			return ErrMissingId
		}

		if r.rule.Metadata.Id == "" {
			log.Error().
				Any("rule", r.rule).
				Msg("Missing rule id")
			return ErrMissingId
		}

		if r.rule.Cre.Category == "" {
			log.Error().
				Any("rule", r.rule).
				Msg("Missing category")
			return ErrMissingCategory
		}

		if _, ok := tags[r.rule.Cre.Category]; !ok {
			log.Error().
				Str("category", r.rule.Cre.Category).
				Msg("Unknown category")
			return ErrUnknownCategory
		}

		for _, tag := range r.rule.Cre.Tags {
			if _, ok := tags[tag]; !ok {
				log.Error().
					Str("tag", tag).
					Msg("Uknown tag")
				return ErrUnknownTag
			}
		}

		if _, ok := allRules[r.rule.Cre.Id]; ok {
			log.Error().
				Str("id", r.rule.Cre.Id).
				Msg("Duplicate rule id")
			return ErrDuplicateRuleId
		}
	}

	return nil
}
