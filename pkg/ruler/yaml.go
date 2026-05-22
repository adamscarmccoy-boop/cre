package ruler

type RuleIncludeT struct {
	Metadata   MetadataT `yaml:"metadata"`
	Tags       []TagT    `yaml:"tags,omitempty"`
	Categories []TagT    `yaml:"categories,omitempty"`
}

type TagT struct {
	Name        string `yaml:"name" binding:"required"`
	DisplayName string `yaml:"displayName" binding:"required"`
	Description string `yaml:"description" binding:"required"`
	Hash        string `yaml:"hash,omitempty"`
	Kind        string `yaml:"kind,omitempty"`
}

type MetadataT struct {
	Kind string `yaml:"kind" binding:"required"`
	Id   string `yaml:"id" binding:"required"`
	Gen  string `yaml:"gen,omitempty"`
}
