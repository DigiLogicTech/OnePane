package localai

// BuiltinCatalog is intentionally small. Production releases can ship a larger,
// signed catalog update without changing recommendation semantics.
func BuiltinCatalog() []ModelSpec {
	return []ModelSpec{
		{ModelRef: "google/gemma-3-1b-it", DisplayName: "Gemma 3 1B IT", Provider: "Google", Architecture: "gemma3", ParamsB: 1, ContextLength: 32768, UseCases: []UseCase{UseRouting, UseChat, UseGeneral}, QualityScore: 55, SourceRef: "hf://google/gemma-3-1b-it", Runtime: "llamacpp", Quantizations: []string{"Q8_0", "Q6_K", "Q5_K_M", "Q4_K_M"}},
		{ModelRef: "microsoft/Phi-4-mini-instruct", DisplayName: "Phi-4 Mini Instruct", Provider: "Microsoft", Architecture: "phi4", ParamsB: 3.8, ContextLength: 131072, UseCases: []UseCase{UseGeneral, UseReasoning, UseRouting}, QualityScore: 72, SourceRef: "hf://microsoft/Phi-4-mini-instruct", Runtime: "llamacpp", Quantizations: []string{"Q8_0", "Q6_K", "Q5_K_M", "Q4_K_M", "Q3_K_M"}},
		{ModelRef: "meta-llama/Llama-3.2-3B-Instruct", DisplayName: "Llama 3.2 3B Instruct", Provider: "Meta", Architecture: "llama", ParamsB: 3, ContextLength: 131072, UseCases: []UseCase{UseGeneral, UseChat, UseRouting}, QualityScore: 68, SourceRef: "hf://meta-llama/Llama-3.2-3B-Instruct", Runtime: "llamacpp", Quantizations: []string{"Q8_0", "Q6_K", "Q5_K_M", "Q4_K_M", "Q3_K_M"}},
		{ModelRef: "Qwen/Qwen2.5-Coder-7B-Instruct", DisplayName: "Qwen2.5 Coder 7B", Provider: "Alibaba", Architecture: "qwen2", ParamsB: 7.6, ContextLength: 131072, UseCases: []UseCase{UseCoding}, QualityScore: 78, SourceRef: "hf://Qwen/Qwen2.5-Coder-7B-Instruct", Runtime: "llamacpp", Quantizations: []string{"Q8_0", "Q6_K", "Q5_K_M", "Q4_K_M", "Q3_K_M", "Q2_K"}},
		{ModelRef: "mistralai/Mistral-Small-3.1-24B-Instruct-2503", DisplayName: "Mistral Small 3.1 24B", Provider: "Mistral", Architecture: "mistral", ParamsB: 24, ContextLength: 131072, UseCases: []UseCase{UseGeneral, UseReasoning, UseCoding}, QualityScore: 88, SourceRef: "hf://mistralai/Mistral-Small-3.1-24B-Instruct-2503", Runtime: "llamacpp", Quantizations: []string{"Q6_K", "Q5_K_M", "Q4_K_M", "Q3_K_M", "Q2_K"}},
		{ModelRef: "nomic-ai/nomic-embed-text-v1.5", DisplayName: "Nomic Embed Text v1.5", Provider: "Nomic", Architecture: "bert", ParamsB: 0.137, ContextLength: 8192, UseCases: []UseCase{UseEmbedding}, QualityScore: 82, SourceRef: "hf://nomic-ai/nomic-embed-text-v1.5", Runtime: "llamacpp", Quantizations: []string{"Q8_0", "Q6_K", "Q5_K_M", "Q4_K_M"}},
	}
}
