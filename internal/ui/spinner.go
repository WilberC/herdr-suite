package ui

// SpinnerFrames is the cycle the hs surfaces animate while work is in flight:
// quadrant blocks, which every monospaced font carries, so a loader never falls
// back to a placeholder box. The caller owns the tick, so a spinner costs
// nothing when nothing is happening.
var SpinnerFrames = []string{"▖", "▘", "▝", "▗"}

// Spinner renders one frame of the cycle on the palette's working accent.
func Spinner(frame int) string {
	if len(SpinnerFrames) == 0 {
		return ""
	}
	return Teal.Render(SpinnerFrames[frame%len(SpinnerFrames)])
}
