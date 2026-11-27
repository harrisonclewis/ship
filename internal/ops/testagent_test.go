package ops

// testAgent stands in for the embedded ship-agent; EnsureAgent refuses an
// empty script.
var testAgent = []byte("#!/usr/bin/env bash\nAGENT_VERSION=test\n")
