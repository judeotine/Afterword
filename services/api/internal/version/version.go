package version

import "runtime/debug"

var (
	Version   = "dev"
	Commit    = ""
	BuildDate = ""
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
}

func Current() Info {
	info := Info{Version: Version, Commit: Commit, BuildDate: BuildDate}
	if build, ok := debug.ReadBuildInfo(); ok {
		info.GoVersion = build.GoVersion
		if info.Commit == "" {
			for _, setting := range build.Settings {
				if setting.Key == "vcs.revision" {
					info.Commit = setting.Value
				}
			}
		}
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	if info.BuildDate == "" {
		info.BuildDate = "unknown"
	}
	return info
}

func (i Info) String() string {
	return i.Version + " (" + i.Commit + ", built " + i.BuildDate + ")"
}
