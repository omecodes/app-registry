package server

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"github.com/gorilla/sessions"
	"github.com/omecodes/app-registry/dao"
	"github.com/omecodes/bome"
	"github.com/omecodes/common/errors"
	"github.com/omecodes/common/grpcx"
	"github.com/omecodes/libome"
	"time"
)

type gRPCHandler struct {
	ome.UnimplementedApplicationsServer
	cookieStore   *sessions.CookieStore
	appsDB        dao.ApplicationsDB
	translationDB *bome.DoubleMap
}

func (g *gRPCHandler) userToken(ctx context.Context, required bool) (*ome.JWT, error) {
	token := ome.TokenFromContext(ctx)
	if token != nil {
		return token, nil
	}

	session, err := grpcx.SessionFromContext(ctx, g.cookieStore, sessionName)
	if err != nil {
		return nil, err
	}

	o := session.Values[sessionKeyJWT]
	if o == nil {
		if required {
			return nil, errors.Forbidden
		}
		return nil, nil
	}

	jwt := o.(string)
	// return service.VerifyJWT(ctx, jwt)
	return ome.ParseJWT(jwt)
}

func (g *gRPCHandler) appCredentials(ctx context.Context) (*ome.Application, error) {
	cred := ome.ProxyCredentialsFromContext(ctx)
	if cred == nil {
		return nil, errors.Forbidden
	}

	a, err := g.appsDB.GetApplication(cred.Key)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, errors.Forbidden
		}
		return nil, err
	}

	if a.Secret != cred.Secret {
		return nil, errors.Forbidden
	}

	return a, nil
}

func (g *gRPCHandler) RegisterApplication(ctx context.Context, in *ome.RegisterApplicationRequest) (*ome.RegisterApplicationResponse, error) {
	a, err := g.appCredentials(ctx)
	if err != nil {
		return nil, err
	}

	if a.Level != ome.ApplicationLevel_Master {
		return nil, errors.Unauthorized
	}

	token, err := g.userToken(ctx, true)
	if err != nil {
		return nil, err
	}

	if in.Application.Id == "ome" && token.Claims.Sub != "ome" {
		return nil, errors.Unauthorized
	}

	in.Application.Info.CreatedBy = token.Claims.Sub
	in.Application.Info.CreatedAt = time.Now().Unix()
	if in.Application.Level != ome.ApplicationLevel_External {
		in.Application.Level = ome.ApplicationLevel_External
	}
	return &ome.RegisterApplicationResponse{}, g.appsDB.SaveApplication(in.Application)
}

func (g *gRPCHandler) DeRegister(ctx context.Context, in *ome.DeRegisterApplicationRequest) (*ome.DeRegisterApplicationResponse, error) {
	a, err := g.appCredentials(ctx)
	if err != nil {
		return nil, err
	}

	if a.Level != ome.ApplicationLevel_Master {
		return nil, errors.Unauthorized
	}

	token, err := g.userToken(ctx, true)
	if err != nil {
		return nil, err
	}
	targetApp, err := g.appsDB.GetApplication(in.ApplicationId)
	if err != nil {
		return nil, err
	}

	if targetApp.Info.CreatedBy != token.Claims.Sub {
		return nil, errors.Unauthorized
	}

	return &ome.DeRegisterApplicationResponse{}, g.appsDB.DeleteApplication(in.ApplicationId)
}

func (g *gRPCHandler) CheckIfExists(ctx context.Context, in *ome.CheckIfExistsRequest) (*ome.CheckIfExistsResponse, error) {
	response := &ome.CheckIfExistsResponse{}
	app, err := g.appsDB.GetApplication(in.ApplicationId)
	response.Exists = app != nil
	return response, err
}

func (g *gRPCHandler) ListApplications(in *ome.ListApplicationsRequest, stream ome.Applications_ListApplicationsServer) error {
	var err error
	var cursor dao.AppCursor

	ctx := stream.Context()
	a, err := g.appCredentials(ctx)
	if err != nil {
		return err
	}

	if a.Level != ome.ApplicationLevel_Root && a.Level != ome.ApplicationLevel_Master {
		a.Secret = ""
		return stream.Send(a)
	}

	if a.Level == ome.ApplicationLevel_Root {
		cursor, err = g.appsDB.ListAllApplications()
		if err != nil {
			return err
		}
	} else {
		token, err := g.userToken(ctx, true)
		if err != nil {
			return err
		}

		cursor, err = g.appsDB.ListApplicationForUser(token.Claims.Sub)
		if err != nil {
			return err
		}
	}

	defer cursor.Close()
	for cursor.HasNext() {
		a, err := cursor.Next()
		if err != nil {
			return err
		}

		a.Secret = ""
		err = stream.Send(a)
		if err != nil {
			return err
		}
	}

	return nil
}

func (g *gRPCHandler) GetApplication(ctx context.Context, in *ome.GetApplicationRequest) (*ome.GetApplicationResponse, error) {
	var user string

	a, err := g.appCredentials(ctx)
	if err != nil {
		return nil, err
	}

	selfDetails := a.Id == in.ApplicationId
	isARootApp := a.Level == ome.ApplicationLevel_Root
	isAMasterApp := a.Level == ome.ApplicationLevel_Master

	if !selfDetails && !isARootApp {
		if !isAMasterApp {
			return nil, errors.Unauthorized
		}

		token, err := g.userToken(ctx, false)
		if err != nil {
			return nil, err
		}

		if token == nil {
			return nil, errors.Unauthorized
		}

		user = token.Claims.Sub
	}

	response := &ome.GetApplicationResponse{}
	response.Application, err = g.appsDB.GetApplication(in.ApplicationId)
	if err != nil {
		return nil, err
	}

	if !isARootApp && !selfDetails && response.Application.Info.CreatedBy != user {
		return nil, errors.Unauthorized
	}

	if response.Application.Id != in.ApplicationId {
		h := md5.Sum([]byte(response.Application.Secret))
		response.Application.Secret = string(h[:])
	} else {
		response.Application.Secret = ""
	}

	return response, err
}

func (g *gRPCHandler) VerifyAuthenticationChallenge(ctx context.Context, in *ome.VerifyAuthenticationChallengeRequest) (*ome.VerifyAuthenticationChallengeResponse, error) {
	response := &ome.VerifyAuthenticationChallengeResponse{}

	a, err := g.appsDB.GetApplication(in.ApplicationId)
	if err != nil {
		return nil, err
	}

	h := hmac.New(sha256.New, []byte(a.Secret))
	nonceBytes, err := hex.DecodeString(in.Nonce)
	if err != nil {
		return nil, err
	}
	h.Write(nonceBytes)

	calculated := hex.EncodeToString(h.Sum(nil))
	response.Verified = calculated == in.Challenge
	return response, nil
}

func (g *gRPCHandler) mustEmbedUnimplementedApplicationsServer() {

}

func NewApplicationServerGRPCHandler(appsDB dao.ApplicationsDB, store *sessions.CookieStore, translationDB *bome.DoubleMap) ome.ApplicationsServer {
	handler := &gRPCHandler{
		cookieStore:   store,
		appsDB:        appsDB,
		translationDB: translationDB,
	}
	var o interface{}
	o = handler
	return o.(ome.ApplicationsServer)
}
