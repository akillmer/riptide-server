package utp

import (
	"expvar"
)

var (
	socketUtpPacketsReceived    = expvar.NewInt("utpSocketUtpPacketsReceived")
	socketNonUtpPacketsReceived = expvar.NewInt("utpSocketNonUtpPacketsReceived")
	nonUtpPacketsDropped        = expvar.NewInt("utpNonUtpPacketsDropped")
	multiMsgRecvs               = expvar.NewInt("utpMultiMsgRecvs")
	singleMsgRecvs              = expvar.NewInt("utpSingleMsgRecvs")
)
