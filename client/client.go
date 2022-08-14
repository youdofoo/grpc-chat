package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"

	"github.com/youdofoo/grpc-chat/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

type sceneMode int

const (
	ModeMenu sceneMode = iota
	ModeChat
	ModeExit
)

const (
	cmdExit = "/exit"
)

var (
	serverAddr = flag.String("addr", "localhost:50051", "The server address in the format of host:port")
)

type scene struct {
	client pb.ChatClient
	mode   sceneMode
}

func (s *scene) run() error {
	fmt.Println("welcom to gRPC chat")
loop:
	for {
		switch s.mode {
		case ModeMenu:
			err := s.loopMenu()
			if err != nil {
				return err
			}
		case ModeChat:
			err := s.loopChat()
			if err != nil {
				return err
			}
		case ModeExit:
			break loop
		}
	}
	return nil
}

func (s *scene) loopMenu() error {
	ctx := context.Background()
loop:
	for {
		fmt.Print("select action (0: start chat, 1: show rooms, 9: exit) > ")
		var input int
		fmt.Scan(&input)
		switch input {
		case 0:
			s.mode = ModeChat
			break loop
		case 1:
			rooms, err := s.client.GetRooms(ctx, &emptypb.Empty{})
			if err != nil {
				return err
			}
			fmt.Println("==== Room List ====")
			for _, r := range rooms.Rooms {
				fmt.Printf("%s\n", r.Name)
			}
			fmt.Println("===================")
		case 9:
			fmt.Println("Bye")
			s.mode = ModeExit
			break loop
		}
	}
	return nil
}

func (s *scene) loopChat() error {
	defer func() { s.mode = ModeMenu }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var roomName, userName string
	fmt.Print("input room name > ")
	fmt.Scan(&roomName)
	fmt.Print("input user name > ")
	fmt.Scan(&userName)

	stream, err := s.client.Chat(ctx)
	if err != nil {
		return err
	}

	err = stream.Send(&pb.ChatMessage{
		Type:     pb.ChatMessageType_JOIN,
		RoomName: roomName,
		UserName: userName,
	})
	if err != nil {
		return err
	}

	errchan := make(chan error, 1)
	waitc := make(chan struct{})
	go func() {
		defer close(waitc)
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				cancel()
				errchan <- err
				return
			}
			fmt.Printf("[%s]: %s\n", msg.UserName, msg.Text)
		}
	}()

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		default:
			fmt.Printf("[%s] > ", userName)
			var text string
			fmt.Scan(&text)
			if text == cmdExit {
				stream.CloseSend()
				break loop
			}
			err = stream.Send(&pb.ChatMessage{
				Type:     pb.ChatMessageType_SEND,
				RoomName: roomName,
				UserName: userName,
				Text:     text,
			})
			if err != nil {
				cancel()
				break loop
			}
		}
	}

	select {
	case recvErr := <-errchan:
		if err == nil {
			err = recvErr
		}
	default:
	}
	<-waitc

	return err
}

func main() {
	flag.Parse()
	conn, err := grpc.Dial(*serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()

	s := &scene{
		client: pb.NewChatClient(conn),
		mode:   ModeMenu,
	}
	err = s.run()
	if err != nil {
		log.Fatalf("exit with error: %v", err)
	}
}
