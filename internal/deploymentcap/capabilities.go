package deploymentcap

type Support string
type Source string

const (
	Supported   Support = "supported"
	Unsupported Support = "unsupported"
	Unknown     Support = "unknown"

	Declared   Source = "declared"
	Discovered Source = "discovered"
	Verified   Source = "verified"
)

type Capability struct {
	State  Support
	Source Source
}

type Set struct {
	TextInput        Capability
	ImageInput       Capability
	AudioInput       Capability
	VideoInput       Capability
	TextOutput       Capability
	ImageOutput      Capability
	AudioOutput      Capability
	VideoOutput      Capability
	ToolCalling      Capability
	StructuredOutput Capability
	Reasoning        Capability
	Streaming        Capability
	Embeddings       Capability
}

type ContextLimits struct {
	Advertised int
	Configured int
	Tested     int
}

func (c ContextLimits) Effective() int {
	values := []int{c.Advertised, c.Configured, c.Tested}
	result := 0
	for _, v := range values {
		if v <= 0 {
			continue
		}
		if result == 0 || v < result {
			result = v
		}
	}
	return result
}

type Requirements struct {
	TextInput      bool
	ImageInput     bool
	AudioInput     bool
	VideoInput     bool
	ToolCalling    bool
	Structured     bool
	MinimumContext int
	CloudAllowed   bool
}

func Eligible(c Set, limits ContextLimits, r Requirements) bool {
	required := []struct {
		needed bool
		cap    Capability
	}{
		{r.TextInput, c.TextInput},
		{r.ImageInput, c.ImageInput},
		{r.AudioInput, c.AudioInput},
		{r.VideoInput, c.VideoInput},
		{r.ToolCalling, c.ToolCalling},
		{r.Structured, c.StructuredOutput},
	}
	for _, item := range required {
		if item.needed && item.cap.State != Supported {
			return false
		}
	}
	return r.MinimumContext <= 0 || limits.Effective() >= r.MinimumContext
}
