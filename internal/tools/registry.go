package tools

import (
	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

// Deps carries everything the tool constructors need.
type Deps struct {
	Client     *abletonosc.Client
	TasteStore tasteStore
	Diagnose   DiagnoseSettings
	Splice     SpliceLibrarySettings
}

// Register defines every tool on g and returns them in a stable order.
func Register(g *genkit.Genkit, deps Deps) []ai.Tool {
	c := deps.Client
	return []ai.Tool{
		// Song / Transport
		NewAbletonTest(g, c),
		NewAbletonPreviewDestructive(g, c),
		NewAbletonDiagnose(g, c, deps.Diagnose),
		NewAbletonGetTempo(g, c),
		NewAbletonSetTempo(g, c),
		NewAbletonPlay(g, c),
		NewAbletonStop(g, c),
		NewAbletonStopAllClips(g, c),
		NewAbletonSetSongKey(g, c),
		NewAbletonSetMetronome(g, c),
		NewAbletonGetSessionSnapshot(g, c),

		// Tracks
		NewAbletonGetTrackNames(g, c),
		NewAbletonGetTrackDevices(g, c),
		NewAbletonCreateMidiTrack(g, c),
		NewAbletonCreateAudioTrack(g, c),
		NewAbletonDuplicateTrack(g, c),
		NewAbletonDeleteTrack(g, c),
		NewAbletonSetTrackName(g, c),
		NewAbletonMuteTrack(g, c),
		NewAbletonSoloTrack(g, c),
		NewAbletonSetTrackVolume(g, c),
		NewAbletonArmTrack(g, c),
		NewAbletonGetTrackInputRouting(g, c),
		NewAbletonSetTrackInputRouting(g, c),
		NewAbletonSetMonitoring(g, c),
		NewAbletonDuplicateTrackForProcessing(g, c),
		NewAbletonGetReturnTracks(g, c),
		NewAbletonCreateReturnTrack(g, c),
		NewAbletonGetTrackSends(g, c),
		NewAbletonSetTrackSend(g, c),
		NewAbletonGetDeviceSidechain(g, c),
		NewAbletonSetDeviceSidechain(g, c),

		// Clips
		NewAbletonCreateClip(g, c),
		NewAbletonGetClipNotes(g, c),
		NewAbletonFireClipSlot(g, c),
		NewAbletonStopClip(g, c),
		NewAbletonClearClipNotes(g, c),
		NewAbletonAddMidiNotes(g, c),
		NewAbletonHumanizeClip(g, c),
		NewAbletonDuplicateClipTo(g, c),
		NewAbletonDeleteClip(g, c),
		NewAbletonSetClipName(g, c),
		NewAbletonGetClipProperties(g, c),
		NewAbletonSetClipPitch(g, c),
		NewAbletonSetClipWarp(g, c),
		NewAbletonSetClipRegion(g, c),
		NewAbletonExtractClipRegion(g, c),
		NewAbletonGetClipEnvelope(g, c),
		NewAbletonSetClipEnvelopeSteps(g, c),
		NewAbletonClearClipEnvelope(g, c),
		NewAbletonMatchClipTempo(g, c),
		NewAbletonAnalyzeLocalAudio(g),
		NewAbletonAnalyzeAudioURL(g),
		NewAbletonChopDraft(g),
		NewAbletonCreateDrumVariation(g, c),
		NewAbletonCreateBassVariation(g, c),
		NewAbletonAuditionAB(g, c),

		// Scenes
		NewAbletonFireScene(g, c),
		NewAbletonGetSceneNames(g, c),
		NewAbletonSetSceneName(g, c),
		NewAbletonCreateNamedScenes(g, c),
		NewAbletonSetSceneClipPresence(g, c),
		NewAbletonCreateSceneEnergyVariation(g, c),
		NewAbletonGetSoundingSnapshot(g, c),

		// Devices / Browser
		NewAbletonGetDeviceParameters(g, c),
		NewAbletonSetDeviceParameter(g, c),
		NewAbletonSetDeviceParameterString(g, c),
		NewAbletonDeleteDevice(g, c),
		NewAbletonGetSimpler(g, c),
		NewAbletonSetSimplerPlaybackMode(g, c),
		NewAbletonSetSimplerSlicing(g, c),
		NewAbletonGetSimplerSlices(g, c),
		NewAbletonSaveSlicePreset(g, c),
		NewAbletonLoadSlicePreset(g, c),
		NewAbletonListSlicePresets(g),
		NewAbletonApplyDeviceIntent(g, c),
		NewAbletonListIntents(g),
		NewAbletonFindBrowserItem(g, c),
		NewAbletonListBrowserFolder(g, c),
		NewAbletonLoadBrowserItem(g, c),
		NewAbletonLoadBrowserPath(g, c),
		NewAbletonLoadDevicePreset(g, c),
		NewAbletonGetSpliceLibrary(g, deps.Splice),
		NewAbletonSearchSpliceSamples(g, deps.Splice),
		NewAbletonLoadSpliceSample(g, c, deps.Splice),

		// Mix bus / Master
		NewAbletonGetTrackMeter(g, c),
		NewAbletonGetMasterMeter(g, c),
		NewAbletonGetMasterVolume(g, c),
		NewAbletonSetMasterVolume(g, c),
		NewAbletonGetMasterDevices(g, c),
		NewAbletonGetMasterDeviceParameters(g, c),
		NewAbletonSetMasterDeviceParameter(g, c),
		NewAbletonLoadOnMaster(g, c),
		NewAbletonAutogainTracks(g, c),
		NewAbletonCaptureMixSnapshot(g, c),
		NewAbletonApplyMixVariation(g, c),
		NewAbletonRestoreMixSnapshot(g, c),

		// Bounce / Session Record
		NewAbletonGetSessionRecord(g, c),
		NewAbletonSetSessionRecord(g, c),
		NewAbletonBounceSessionPass(g, c),

		// Recipes
		NewAbletonSetupDrumTrack(g, c),
		NewAbletonCompareABVariation(g, c),
		NewAbletonCompareFXBypass(g, c),
		NewAbletonBuildChordClip(g, c),

		// A/B comparison feedback
		NewAbletonRecordVariationPreference(g, deps.TasteStore),
		NewAbletonGetTasteProfile(g, deps.TasteStore),

		// Raw OSC
		NewAbletonOscSend(g, c),
	}
}
