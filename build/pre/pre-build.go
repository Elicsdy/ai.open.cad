package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
)

const binaryName = "aiOpenCAD"

type JsonDaemonConfig struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Icon        string   `json:"icon"`
	Pkg         string   `json:"pkg"`
	Interpreter string   `json:"interpreter"`
	Version     string   `json:"version"`
	Path        string   `json:"path"`
	Args        []string `json:"args"`
	OS          string   `json:"os"`
	Arch        string   `json:"arch"`
	Port        int      `json:"port"`
	PortType    string   `json:"port_type"`
	Doc         string   `json:"doc"`
	ApiDoc      string   `json:"api_doc"`
	Homepage    string   `json:"homepage"`
	Datas       []string `json:"datas"`
	Logs        []string `json:"logs"`
	Configs     []struct {
		Title string `json:"title"`
		Type  string `json:"type"`
		Path  string `json:"path"`
	} `json:"configs"`
	FirewallEnable bool     `json:"firewall_enable"`
	FirewallPorts  []string `json:"firewall_ports"`
	Gateway        []struct {
		Enable        bool        `json:"enable"`
		GatewayType   string      `json:"gatewayType"`
		ApiPath       string      `json:"apiPath"`
		ApiPrefix     string      `json:"apiPrefix"`
		RemovePrefix  bool        `json:"removePrefix"`
		ApiPermission interface{} `json:"apiPermission"`
	} `json:"gateway"`
}

func ReadFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	return io.ReadAll(file)
}

func WriteFile(path string, data string) error {
	return os.WriteFile(path, []byte(data), 0666)
}

func WriteBytes(path string, data []byte) error {
	return os.WriteFile(path, data, 0666)
}

func parseFlags() (string, string, string) {
	goos := flag.String("os", runtime.GOOS, "The operating system")
	goarch := flag.String("arch", runtime.GOARCH, "The architecture")
	tag := flag.String("tag", "0.0.0", "The tag of docker image")
	flag.Parse()
	return *goos, *goarch, *tag
}

func loadDaemonConfig(path string) (JsonDaemonConfig, error) {
	var daemonConfig JsonDaemonConfig
	fileData, err := ReadFile(path)
	if err != nil {
		return JsonDaemonConfig{}, err
	}
	if err := json.Unmarshal(fileData, &daemonConfig); err != nil {
		return JsonDaemonConfig{}, err
	}
	return daemonConfig, nil
}

func applyDaemonRuntimeFields(daemonConfig *JsonDaemonConfig, goos, goarch, tag string) {
	daemonConfig.Version = tag
	daemonConfig.OS = goos
	daemonConfig.Arch = goarch
	if goos == "windows" {
		daemonConfig.Path = "./" + binaryName + ".exe"
		return
	}
	daemonConfig.Path = "./" + binaryName
}

func writeDaemonJSON(path string, daemonConfig JsonDaemonConfig) error {
	data, err := json.MarshalIndent(daemonConfig, "", "  ")
	if err != nil {
		return err
	}
	return WriteBytes(path, data)
}

func copyFile(src, dst string) error {
	fileData, err := ReadFile(src)
	if err != nil {
		return err
	}
	return WriteBytes(dst, fileData)
}

func writeReleaseConfig(path string) error {
	data := `{
  "addr": ":15566",
  "dbPath": "./data/app.db",
  "frontendDist": "./dist",
  "llm": {
    "baseUrl": "",
    "apiKey": "",
    "model": "gpt-4.1-mini",
    "timeout": "120s"
  }
}
`
	return WriteFile(path, data)
}

func main() {
	goos, goarch, tag := parseFlags()
	daemonCfgPath := "./build/base-daemon.json"

	daemonConfig, err := loadDaemonConfig(daemonCfgPath)
	if err != nil {
		fmt.Printf("read/parse daemon config from %s failed, err:%v\n", daemonCfgPath, err)
		return
	}

	applyDaemonRuntimeFields(&daemonConfig, goos, goarch, tag)

	if err := writeDaemonJSON("./daemon.json", daemonConfig); err != nil {
		fmt.Printf("write file to ./daemon.json failed, err:%v\n", err)
	} else {
		fmt.Println("write file to ./daemon.json success")
	}

	if err := copyFile("./build/base-favicon.ico", "./favicon.ico"); err != nil {
		fmt.Printf("read file from ./build/base-favicon.ico failed, err:%v\n", err)
		return
	}
	fmt.Println("write file to ./favicon.ico success")

	if err := writeReleaseConfig("./build/release.config.json"); err != nil {
		fmt.Printf("write file to ./build/release.config.json failed, err:%v\n", err)
		return
	}
	fmt.Println("write file to ./build/release.config.json success")
}
