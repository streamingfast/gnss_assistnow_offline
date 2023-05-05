module gnss_assistnow_offline

go 1.20

replace (
	github.com/daedaleanai/ublox => /Users/cbillett/devel/github/ublox
)

require (
	github.com/daedaleanai/ublox v0.0.0-20210116232802-16609b0f9f43
	github.com/tarm/serial v0.0.0-20180830185346-98f6abe2eb07
)

require golang.org/x/sys v0.7.0 // indirect
