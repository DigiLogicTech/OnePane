package deploymentcap

import "testing"

func TestEffectiveContextUsesSmallestRuntimeLimit(t *testing.T) {
	c := ContextLimits{Advertised: 131072, Configured: 65536, Tested: 98304}
	if got := c.Effective(); got != 65536 {
		t.Fatalf("effective context = %d", got)
	}
}

func TestUnknownDoesNotSatisfyHardRequirement(t *testing.T) {
	caps := Set{
		TextInput:  Capability{State: Supported, Source: Verified},
		ImageInput: Capability{State: Unknown, Source: Discovered},
	}
	if Eligible(caps, ContextLimits{Configured: 65536}, Requirements{TextInput: true, ImageInput: true}) {
		t.Fatal("unknown image capability passed hard filter")
	}
}

func TestSupportedRequirementsPass(t *testing.T) {
	caps := Set{
		TextInput:   Capability{State: Supported, Source: Verified},
		ToolCalling: Capability{State: Supported, Source: Declared},
	}
	if !Eligible(caps, ContextLimits{Configured: 65536}, Requirements{TextInput: true, ToolCalling: true, MinimumContext: 32768}) {
		t.Fatal("eligible deployment rejected")
	}
}
