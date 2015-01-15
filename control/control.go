package control

import (
	"bufio"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/intelsdilabs/pulse/control/plugin"
)

// control private key (RSA private key)
// control public key (RSA public key)
// Plugin token = token generated by plugin and passed to control
// Session token = plugin seed encrypted by control private key, verified by plugin using control public key
//

const (
	// LoadedPlugin States
	DetectedState pluginState = "detected"
	LoadingState  pluginState = "loading"
	LoadedState   pluginState = "loaded"
	UnloadedState pluginState = "unloaded"
)

type pluginState string

type pluginType int

type loadedPlugins []LoadedPlugin

type executablePlugins []ExecutablePlugin

// A interface representing an executable plugin.
type PluginExecutor interface {
	Kill() error
	Wait() error
	ResponseReader() io.Reader
}

// Represents a plugin loaded or loading into control
type LoadedPlugin struct {
	Meta       plugin.PluginMeta
	Path       string
	Type       plugin.PluginType
	State      pluginState
	Token      string
	LoadedTime time.Time
}

type pluginControl struct {
	// TODO, going to need coordination on changing of these
	LoadedPlugins  loadedPlugins
	RunningPlugins executablePlugins
	Started        bool

	// loadRequestsChan chan LoadedPlugin

	controlPrivKey *rsa.PrivateKey
	controlPubKey  *rsa.PublicKey
}

func (p *pluginControl) GenerateArgs(daemon bool) plugin.Arg {
	a := plugin.Arg{
		ControlPubKey: p.controlPubKey,
		PluginLogPath: "/tmp",
		RunAsDaemon:   daemon,
	}
	return a
}

func Control() *pluginControl {
	c := new(pluginControl)
	// c.loadRequestsChan = make(chan LoadedPlugin)
	// privatekey, err := rsa.GenerateKey(rand.Reader, 4096)

	// if err != nil {
	// 	panic(err)
	// }

	// // Future use for securing.
	// c.controlPrivKey = privatekey
	// c.controlPubKey = &privatekey.PublicKey

	return c
}

// Begin handling load, unload, and inventory
func (p *pluginControl) Start() {
	// begin controlling

	// Start load handler. We only start one to keep load requests handled in
	// a linear fashion for now as this is a low priority.
	// go p.HandleLoadRequests()

	p.Started = true
}

func (p *pluginControl) Stop() {
	// close(p.loadRequestsChan)
	p.Started = false
}

func (p *pluginControl) Load(path string) (*LoadedPlugin, error) {
	if !p.Started {
		return nil, errors.New("Must start plugin control before calling Load()")
	}

	/*
		Loading plugin status

		Before start (todo)
		* executable (caught on start)
		* signed? (todo)
		* Grab checksum (file watching? todo)
		=> Plugin state = detected

		After start before Ping
		* starts? (catch crash)
		* response? (catch stdout)
		=> Plugin state = loaded
	*/

	log.Printf("Attempting to load: %s\v", path)
	lPlugin := new(LoadedPlugin)
	lPlugin.Path = path
	lPlugin.State = DetectedState

	// Create a new Executable plugin
	//
	// In this case we only support Linux right now
	ePlugin, err := newExecutablePlugin(p, lPlugin.Path, false)

	// If error then log and return
	if err != nil {
		log.Println(err)
		return nil, err
	}

	// Start the plugin using the start method
	err = ePlugin.Start()
	if err != nil {
		log.Println(err)
		return nil, err
	}

	var resp *plugin.Response
	// This blocks until a response or an error
	resp, err = waitForResponse(ePlugin, time.Second*3)
	// resp, err = WaitForPluginResponse(ePlugin, time.Second*3)

	// If error then we log and return
	if err != nil {
		log.Println(err)
		return nil, err
	}

	// If the response state is not Success we log an error
	if resp.State != plugin.PluginSuccess {
		log.Printf("Plugin loading did not succeed: %s\n", resp.ErrorMessage)
		return nil, errors.New(fmt.Sprintf("Plugin loading did not succeed: %s\n", resp.ErrorMessage))
	}
	// On response we create a LoadedPlugin
	// and add to LoadedPlugins index
	//
	lPlugin.Meta = resp.Meta
	lPlugin.Type = resp.Type
	lPlugin.Token = resp.Token
	lPlugin.LoadedTime = time.Now()
	lPlugin.State = LoadedState

	/*

		Name
		Version
		Loaded Time

	*/

	return lPlugin, err
}

// Wait for response from started ExecutablePlugin. Returns plugin.Response or error.
func waitForResponse(p PluginExecutor, timeout time.Duration) (*plugin.Response, error) {
	// The response we want to return

	var resp *plugin.Response = new(plugin.Response)
	var timeoutErr error
	var jsonErr error

	// Kill on timeout
	go func() {
		time.Sleep(timeout)
		timeoutErr = errors.New("Timeout waiting for response")
		p.Kill()
		return
	}()

	// Wait for response from ResponseReader
	scanner := bufio.NewScanner(p.ResponseReader())
	go func() {
		for scanner.Scan() {
			// Get bytes
			b := scanner.Bytes()
			// attempt to unmarshall into struct
			err := json.Unmarshal(b, resp)
			if err != nil {
				jsonErr = errors.New("JSONError - " + err.Error())
				return
			}
		}
	}()

	// Wait for PluginExecutor to respond
	err := p.Wait()
	// Return top level error
	if jsonErr != nil {
		return nil, jsonErr
	}
	// Return top level error
	if timeoutErr != nil {
		return nil, timeoutErr
	}
	// Return pExecutor.Wait() error
	if err != nil {
		// log.Printf("[CONTROL] Plugin stopped with error [%v]\n", err)
		return nil, err
	}
	// Return response
	return resp, nil
}