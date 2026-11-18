package application

import "time"

const MaxProbeNodes = 64
const ProbeConcurrency = 2
const ProbeTimeout = 12 * time.Second
const ProbeTaskTimeout = time.Duration((MaxProbeNodes+ProbeConcurrency-1)/ProbeConcurrency)*ProbeTimeout + 2*time.Minute
const ControlTaskTimeout = 5 * time.Minute
