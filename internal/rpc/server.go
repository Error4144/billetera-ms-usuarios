package rpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"strings"
	"time"

	pb "billetera.local/ms-usuarios/gen/billetera/usuarios/v1"
	"billetera.local/ms-usuarios/internal/users"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	BFFIdentity    = "spiffe://billetera/bff-graphql"
	HealthIdentity = "spiffe://billetera/ms-usuarios-health"
	ServiceName    = "billetera.usuarios.v1.UsersService"
)

type Registrar interface {
	Register(context.Context, users.RegisterInput) (users.Registration, error)
	Login(context.Context, string, string) (users.LoginResult, error)
	ValidateSession(context.Context, string) (users.Session, error)
	Logout(context.Context, string) error
	GetProfile(context.Context, string) (users.Profile, error)
	UpdateProfile(context.Context, string, string, int64) (users.Profile, error)
	GetPreferences(context.Context, string) (users.Preferences, error)
	UpdatePreferences(context.Context, string, users.Preferences, int64) (users.Preferences, error)
}

var _ Registrar = (*users.Service)(nil)

func protocolError(err error) error {
	switch {
	case errors.Is(err, users.ErrInvalid):
		return status.Error(codes.InvalidArgument, "datos inválidos")
	case errors.Is(err, users.ErrEmailExists):
		return status.Error(codes.AlreadyExists, "correo ya registrado")
	case errors.Is(err, users.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "credenciales o sesión inválidas")
	case errors.Is(err, users.ErrRateLimited):
		return status.Error(codes.ResourceExhausted, "límite temporal de acceso alcanzado")
	case errors.Is(err, users.ErrConflict):
		return status.Error(codes.Aborted, "versión desactualizada; vuelve a consultar")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "solicitud cancelada")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "tiempo de solicitud agotado")
	default:
		return status.Error(codes.Internal, "no se pudo completar la operación")
	}
}
func sessionToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", protocolError(users.ErrUnauthenticated)
	}
	values := md.Get("authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") || len(values[0]) != 50 {
		return "", protocolError(users.ErrUnauthenticated)
	}
	return strings.TrimPrefix(values[0], "Bearer "), nil
}
func sessionResponse(session users.Session) *pb.SessionIdentity {
	return &pb.SessionIdentity{UserId: session.UserID, SessionId: session.ID, ExpiresAt: timestamppb.New(session.ExpiresAt)}
}
func profileResponse(profile users.Profile) *pb.UserProfile {
	return &pb.UserProfile{Id: profile.ID, Email: profile.Email, DisplayName: profile.Name, Version: profile.Version, CreatedAt: timestamppb.New(profile.CreatedAt), UpdatedAt: timestamppb.New(profile.UpdatedAt)}
}
func preferencesResponse(p users.Preferences) *pb.UserPreferences {
	return &pb.UserPreferences{Currency: p.Currency, TimeZone: p.TimeZone, MicroExpenseMaximumAmount: p.MaximumAmount, MicroExpenseMinimumCount: p.MinimumCount, Version: p.Version, UpdatedAt: timestamppb.New(p.UpdatedAt)}
}

func (h *handler) Login(ctx context.Context, request *pb.LoginRequest) (*pb.LoginResponse, error) {
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		return nil, status.Error(codes.ResourceExhausted, "capacidad de acceso temporalmente agotada")
	}
	result, err := h.registrar.Login(ctx, request.GetEmail(), request.GetPassword())
	if err != nil {
		return nil, protocolError(err)
	}
	return &pb.LoginResponse{SessionToken: result.Token, Session: sessionResponse(result.Session)}, nil
}
func (h *handler) ValidateSession(ctx context.Context, _ *emptypb.Empty) (*pb.SessionIdentity, error) {
	token, err := sessionToken(ctx)
	if err != nil {
		return nil, err
	}
	result, err := h.registrar.ValidateSession(ctx, token)
	if err != nil {
		return nil, protocolError(err)
	}
	return sessionResponse(result), nil
}
func (h *handler) Logout(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	token, err := sessionToken(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.registrar.Logout(ctx, token); err != nil {
		return nil, protocolError(err)
	}
	return &emptypb.Empty{}, nil
}
func (h *handler) GetProfile(ctx context.Context, _ *emptypb.Empty) (*pb.UserProfile, error) {
	token, err := sessionToken(ctx)
	if err != nil {
		return nil, err
	}
	result, err := h.registrar.GetProfile(ctx, token)
	if err != nil {
		return nil, protocolError(err)
	}
	return profileResponse(result), nil
}
func (h *handler) UpdateProfile(ctx context.Context, request *pb.UpdateProfileRequest) (*pb.UserProfile, error) {
	token, err := sessionToken(ctx)
	if err != nil {
		return nil, err
	}
	result, err := h.registrar.UpdateProfile(ctx, token, request.GetDisplayName(), request.GetExpectedVersion())
	if err != nil {
		return nil, protocolError(err)
	}
	return profileResponse(result), nil
}
func (h *handler) GetPreferences(ctx context.Context, _ *emptypb.Empty) (*pb.UserPreferences, error) {
	token, err := sessionToken(ctx)
	if err != nil {
		return nil, err
	}
	result, err := h.registrar.GetPreferences(ctx, token)
	if err != nil {
		return nil, protocolError(err)
	}
	return preferencesResponse(result), nil
}
func (h *handler) UpdatePreferences(ctx context.Context, request *pb.UpdatePreferencesRequest) (*pb.UserPreferences, error) {
	token, err := sessionToken(ctx)
	if err != nil {
		return nil, err
	}
	input := users.Preferences{Currency: request.GetCurrency(), TimeZone: request.GetTimeZone(), MaximumAmount: request.GetMicroExpenseMaximumAmount(), MinimumCount: request.GetMicroExpenseMinimumCount()}
	result, err := h.registrar.UpdatePreferences(ctx, token, input, request.GetExpectedVersion())
	if err != nil {
		return nil, protocolError(err)
	}
	return preferencesResponse(result), nil
}

type Pinger interface{ Ping(context.Context) error }
type TLSFiles struct{ Certificate, Key, CA string }

// New requires both a verified client certificate and an authorized URI SAN.
func New(registrar Registrar, database Pinger, files TLSFiles) (*grpc.Server, error) {
	cert, err := tls.LoadX509KeyPair(files.Certificate, files.Key)
	if err != nil {
		return nil, errors.New("certificado o clave TLS inválidos")
	}
	roots, err := loadCA(files.CA)
	if err != nil {
		return nil, err
	}
	transport := credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert})
	server := grpc.NewServer(grpc.Creds(transport), grpc.MaxRecvMsgSize(16*1024), grpc.ConnectionTimeout(5*time.Second), grpc.UnaryInterceptor(unary),
		grpc.StreamInterceptor(func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			if err := authorize(stream.Context(), info.FullMethod); err != nil {
				return err
			}
			return handler(srv, stream)
		}))
	pb.RegisterUsersServiceServer(server, &handler{registrar: registrar, slots: make(chan struct{}, 4)})
	healthpb.RegisterHealthServer(server, &health{database: database})
	return server, nil
}
func loadCA(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("CA TLS no disponible")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(data) {
		return nil, errors.New("CA TLS inválida")
	}
	return roots, nil
}
func ClientCredentials(files TLSFiles, serverName string) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(files.Certificate, files.Key)
	if err != nil {
		return nil, errors.New("certificado de cliente inválido")
	}
	roots, err := loadCA(files.CA)
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}, ServerName: serverName}), nil
}
func authorize(ctx context.Context, method string) error {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "se requiere identidad de servicio")
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.PeerCertificates) == 0 {
		return status.Error(codes.Unauthenticated, "se requiere TLS mutuo verificado")
	}
	for _, identity := range info.State.PeerCertificates[0].URIs {
		if identity.String() == BFFIdentity || (identity.String() == HealthIdentity && method == healthpb.Health_Check_FullMethodName) {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "servicio sin permiso para esta operación")
}
func unary(ctx context.Context, request any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (response any, err error) {
	defer func() {
		if recover() != nil {
			response = nil
			err = status.Error(codes.Internal, "fallo interno de Usuarios")
		}
	}()
	if err = authorize(ctx, info.FullMethod); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return next(ctx, request)
}

type handler struct {
	pb.UnimplementedUsersServiceServer
	registrar Registrar
	slots     chan struct{}
}

func (h *handler) RegisterUser(ctx context.Context, request *pb.RegisterUserRequest) (*pb.RegisterUserResponse, error) {
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		return nil, status.Error(codes.ResourceExhausted, "capacidad de registro temporalmente agotada")
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "datos de registro inválidos")
	}
	result, err := h.registrar.Register(ctx, users.RegisterInput{Email: request.GetEmail(), Password: request.GetPassword(), Name: request.GetDisplayName()})
	if err != nil {
		switch {
		case errors.Is(err, users.ErrInvalid):
			return nil, status.Error(codes.InvalidArgument, "datos de registro inválidos")
		case errors.Is(err, users.ErrEmailExists):
			return nil, status.Error(codes.AlreadyExists, "correo ya registrado")
		case errors.Is(err, context.Canceled):
			return nil, status.Error(codes.Canceled, "solicitud cancelada")
		case errors.Is(err, context.DeadlineExceeded):
			return nil, status.Error(codes.DeadlineExceeded, "tiempo de registro agotado")
		default:
			return nil, status.Error(codes.Internal, "no se pudo completar el registro")
		}
	}
	return &pb.RegisterUserResponse{Profile: &pb.UserProfile{Id: result.Profile.ID, Email: result.Profile.Email, DisplayName: result.Profile.Name, Version: result.Profile.Version, CreatedAt: timestamppb.New(result.Profile.CreatedAt), UpdatedAt: timestamppb.New(result.Profile.UpdatedAt)},
		Preferences: &pb.UserPreferences{Currency: result.Preferences.Currency, TimeZone: result.Preferences.TimeZone, MicroExpenseMaximumAmount: result.Preferences.MaximumAmount, MicroExpenseMinimumCount: result.Preferences.MinimumCount, Version: result.Preferences.Version, UpdatedAt: timestamppb.New(result.Preferences.UpdatedAt)}}, nil
}

type health struct {
	healthpb.UnimplementedHealthServer
	database Pinger
}

func (h *health) Check(ctx context.Context, request *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	if request.GetService() != "" && request.GetService() != ServiceName {
		return nil, status.Error(codes.NotFound, "servicio desconocido")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	state := healthpb.HealthCheckResponse_SERVING
	if h.database.Ping(ctx) != nil {
		state = healthpb.HealthCheckResponse_NOT_SERVING
	}
	return &healthpb.HealthCheckResponse{Status: state}, nil
}
