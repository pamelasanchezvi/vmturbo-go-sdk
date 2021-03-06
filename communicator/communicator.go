package communicator

import (
	"encoding/base64"
	"net/http"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/websocket"
)

// An interface to handle server request.
type ServerMessageHandler interface {
	AddTarget()
	Validate(serverMsg *MediationServerMessage)
	DiscoverTopology(serverMsg *MediationServerMessage)
	HandleAction(serverMsg *MediationServerMessage)
}

type WebSocketCommunicator struct {
	VmtServerAddress string
	LocalAddress     string
	ServerUsername   string
	ServerPassword   string
	ServerMsgHandler ServerMessageHandler
	ws               *websocket.Conn
}

// Handle server message according to serverMessage type
func (wsc *WebSocketCommunicator) handleServerMessage(serverMsg *MediationServerMessage) {
	if wsc.ServerMsgHandler == nil {
		// Log the error
		glog.V(4).Infof("Server Message Handler is nil")
		return
	}
	glog.V(3).Infof("Receive message from server. Unmarshalled to: %+v", serverMsg)

	// if serverMsg.GetAck() != nil && clientMsg.GetContainerInfo() != nil {
	// 	glog.V(3).Infof("VMTurbo server acknowledged, connection established and adding target.")
	// 	// Add current Kuberenetes target.
	// 	wsc.ServerMsgHandler.AddTarget()

	// } else
	if serverMsg.GetValidationRequest() != nil {
		wsc.ServerMsgHandler.Validate(serverMsg)
	} else if serverMsg.GetDiscoveryRequest() != nil {
		wsc.ServerMsgHandler.DiscoverTopology(serverMsg)
	} else if serverMsg.GetActionRequest() != nil {
		wsc.ServerMsgHandler.HandleAction(serverMsg)
	}
}

func (wsc *WebSocketCommunicator) SendClientMessage(clientMsg *MediationClientMessage) {
	glog.V(3).Infof("Send Client Message: %+v", clientMsg)
	wsc.sendMessage(clientMsg)
}

func (wsc *WebSocketCommunicator) SendRegistrationMessage(containerInfo *ContainerInfo) {
	glog.V(3).Infof("Send registration message: %+v", containerInfo)
	wsc.sendMessage(containerInfo)
}

func (wsc *WebSocketCommunicator) sendMessage(message proto.Message) {
	msgMarshalled, err := proto.Marshal(message)
	if err != nil {
		glog.Fatal("marshaling error: ", err)
	}
	if wsc.ws == nil {
		glog.Errorf("web socket is nil")
		return
	}
	if msgMarshalled == nil {
		glog.Errorf("marshalled msg is nil")
		return
	}
	websocket.Message.Send(wsc.ws, msgMarshalled)
}

func (wsc *WebSocketCommunicator) CloseAndRegisterAgain(containerInfo *ContainerInfo) {
	if wsc.ws != nil {
		//Close the socket if it's not nil to prevent socket leak
		wsc.ws.Close()
		wsc.ws = nil
	}
	for wsc.ws == nil {
		time.Sleep(time.Second * 10)
		wsc.RegisterAndListen(containerInfo)
	}

}

// Register target type on vmt server and start to listen for server message
func (wsc *WebSocketCommunicator) RegisterAndListen(containerInfo *ContainerInfo) {
	// vmtServerUrl := "ws://10.10.173.154:8080/vmturbo/remoteMediation"
	vmtServerUrl := "ws://" + wsc.VmtServerAddress + "/vmturbo/remoteMediation"
	localAddr := wsc.LocalAddress

	glog.V(3).Infof("Dial Server: %s", vmtServerUrl)

	config, err := websocket.NewConfig(vmtServerUrl, localAddr)
	if err != nil {
		glog.Fatal(err)
	}
	usrpasswd := []byte(wsc.ServerUsername + ":" + wsc.ServerPassword)

	config.Header = http.Header{
		"Authorization": {"Basic " + base64.StdEncoding.EncodeToString(usrpasswd)},
	}
	webs, err := websocket.DialConfig(config)

	// webs, err := websocket.Dial(vmtServerUrl, "", localAddr)
	if err != nil {
		glog.Error(err)
		if webs == nil {
			glog.Error("The websocket is null, reset")
		}
		wsc.CloseAndRegisterAgain(containerInfo)
	}
	wsc.ws = webs

	glog.V(3).Infof("Send registration info")
	wsc.SendRegistrationMessage(containerInfo)

	time.Sleep(3000 * time.Millisecond)
	glog.Info("Add target.")
	wsc.ServerMsgHandler.AddTarget()

	var msg = make([]byte, 1024)
	var n int

	// main loop for listening server message.
	for {
		glog.Info("Waiting from server.")
		if n, err = wsc.ws.Read(msg); err != nil {
			glog.Error(err)
			//glog.Fatal(err.Error())
			//re-establish connection when error
			glog.V(3).Infof("Error happened, re-establish websocket connection")
			break
		}

		// glog.Info("Unmarshalled msg is: %v", msg)

		// ack := &Ack{}
		// err = proto.Unmarshal(msg[:n], ack)
		// glog.V(3).Infof("Ack from server: %v", ack)

		serverMsg := &MediationServerMessage{}
		err = proto.Unmarshal(msg[:n], serverMsg)
		if err != nil {
			glog.Error("Received unmarshalable error, please make sure you are running the latest VMT server")
			glog.Fatal("unmarshaling error: ", err)
		}

		//Spawn a separate go routine to handle the server message
		go wsc.handleServerMessage(serverMsg)
		glog.V(3).Infof("Continue listen from server...")
	}
	wsc.CloseAndRegisterAgain(containerInfo)
}
