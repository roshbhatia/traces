// Package provider narrows the canonical provider/v1 contract to Traces.
//
// The contract itself is provider.cue in roshbhatia/provider-spec, pinned as
// the provider-spec flake input. This file adds only the Traces action
// vocabulary: a manifest may declare no action outside #ActionName. Vet a
// manifest with both files:
//
//	cue vet -d '#Manifest' "$PROVIDER_SPEC/provider.cue" schema/narrow.cue extras/git/provider.yaml
package provider

#ActionName: "activity.read" |
	"clipboard.write" |
	"diff.render" |
	"document.open" |
	"provider.validate" |
	"session.current" |
	"session.discover"

// The spec's pattern constraint admits any lowercase name, and a closed struct
// unified with it widens rather than narrows. The hidden field checks each
// declared name against the vocabulary instead.
#Manifest: actions: [name=string]: {_supported: name & #ActionName}
