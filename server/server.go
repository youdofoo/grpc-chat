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
)

type room struct {
	name     string
	messages []string
	streams  []pb.Chat_ChatServer
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	rooms := make([]*pb.Room, 0, len(s.rooms))
	for k := range s.rooms {
		rooms = append(rooms, &pb.Room{Name: k})
	}
	return &pb.Rooms{Rooms: rooms}, nil
}

func (s *chatServer) Chat(stream pb.Chat_ChatServer) error {
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			// Client leaved
			return nil
		}
		r := s.lookupRoom(msg.RoomName)
		if r == nil {
			r = s.createRoom(msg.RoomName, stream)
		}
		s.mu.Lock()
		r.messages = append(r.messages, msg.Text)
		for _, stream := range r.streams {
			err = stream.Send(&pb.ChatMessage{Text: msg.Text})
			if err != nil {
				s.mu.Unlock()
				return err
			}
		}
		s.mu.Unlock()
	}
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
		name:    name,
		streams: []pb.Chat_ChatServer{stream},
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
