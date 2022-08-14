package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/youdofoo/grpc-chat/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type room struct {
	name     string
	messages []*pb.ChatMessage
	users    map[string]*user
}

type user struct {
	name   string
	stream pb.Chat_ChatServer
}

type chatServer struct {
	pb.UnimplementedChatServer

	mu    sync.RWMutex
	rooms map[string]*room
}

func newServer() pb.ChatServer {
	return &chatServer{
		rooms: make(map[string]*room),
	}
}

func (s *chatServer) GetRooms(ctx context.Context, in *empty.Empty) (*pb.Rooms, error) {
	log.Printf("[INFO] GetRooms called\n")
	s.mu.RLock()
	defer s.mu.RUnlock()
	rooms := make([]*pb.Room, 0, len(s.rooms))
	for k := range s.rooms {
		rooms = append(rooms, &pb.Room{Name: k})
	}
	return &pb.Rooms{Rooms: rooms}, nil
}

func (s *chatServer) Chat(stream pb.Chat_ChatServer) error {
	var roomName string
	var userName string

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			// Client leaved
			return nil
		}
		if err != nil {
			return err
		}
		switch msg.Type {
		case pb.ChatMessageType_JOIN:
			roomName = msg.RoomName
			userName = msg.UserName
			err = s.join(roomName, userName, stream)
			if err != nil {
				return err
			}
			defer func() {
				s.leave(roomName, userName)
			}()
		case pb.ChatMessageType_SEND:
			if msg.RoomName != roomName || msg.UserName != userName {
				// ignore invalid room or user
				log.Println("sent room name or user name is wrong")
				continue
			}
			err = s.send(msg)
			if err != nil {
				return err
			}
		}
	}
}

func (s *chatServer) join(roomName, userName string, stream pb.Chat_ChatServer) error {
	r := s.lookupRoom(roomName)
	if r == nil {
		r = s.createRoom(roomName, stream)
	}
	s.mu.Lock()
	if _, ok := r.users[userName]; ok {
		s.mu.Unlock()
		return status.Errorf(codes.AlreadyExists, "user %s already exists in room %s", userName, roomName)
	}
	r.users[userName] = &user{
		name:   userName,
		stream: stream,
	}
	s.mu.Unlock()

	// send all past messages
	s.mu.RLock()
	for _, msg := range r.messages {
		err := stream.Send(msg)
		if err != nil {
			return err
		}
	}
	s.mu.RUnlock()

	log.Printf("[INFO] %s joined to %s\n", userName, roomName)

	return nil
}

func (s *chatServer) send(msg *pb.ChatMessage) error {
	r := s.lookupRoom(msg.RoomName)
	if r == nil {
		return status.Errorf(codes.NotFound, "room %s not found", msg.RoomName)
	}
	s.mu.Lock()
	r.messages = append(r.messages, msg)
	for _, user := range r.users {
		err := user.stream.Send(msg)
		if err != nil {
			s.mu.Unlock()
			return err
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *chatServer) leave(roomName, userName string) {
	log.Printf("[INFO] %s leaved from %s\n", userName, roomName)
	r := s.lookupRoom(roomName)
	if r == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(r.users, userName)
}

func (s *chatServer) lookupRoom(name string) *room {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rooms[name]
}

func (s *chatServer) createRoom(name string, stream pb.Chat_ChatServer) *room {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rooms[name] = &room{
		name:  name,
		users: make(map[string]*user),
	}
	return s.rooms[name]
}

var (
	port = flag.Int("port", 50051, "The server port")
)

func main() {
	flag.Parse()
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterChatServer(grpcServer, newServer())
	grpcServer.Serve(lis)
}
