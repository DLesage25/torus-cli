package api

import (
	"context"
	"net/url"
	"time"

	"github.com/manifoldco/torus-cli/apitypes"
	"github.com/manifoldco/torus-cli/envelope"
	"github.com/manifoldco/torus-cli/identity"
	"github.com/manifoldco/torus-cli/primitive"
)

// upstreamInvitesClient makes proxied requests to the registry's org invites
// endpoints
type upstreamOrgInvitesClient struct {
	client RoundTripper
}

// OrgInvitesClient makes requests to the registry's and daemon's org invites
// endpoints
type OrgInvitesClient struct {
	upstreamOrgInvitesClient
	client *apiRoundTripper
}

func newOrgInvitesClient(rt *apiRoundTripper) *OrgInvitesClient {
	return &OrgInvitesClient{upstreamOrgInvitesClient{rt}, rt}
}

// List all invites for a given org
func (i *upstreamOrgInvitesClient) List(ctx context.Context, orgID *identity.ID, states []string) ([]envelope.OrgInvite, error) {
	v := &url.Values{}
	v.Set("org_id", orgID.String())

	for _, state := range states {
		v.Add("state", state)
	}

	req, err := i.client.NewRequest("GET", "/org-invites", v, nil)
	if err != nil {
		return nil, err
	}

	invites := []envelope.OrgInvite{}
	_, err = i.client.Do(ctx, req, &invites)
	return invites, err
}

// Send creates a new org invitation
func (i *upstreamOrgInvitesClient) Send(ctx context.Context, email string, orgID, inviterID identity.ID, teamIDs []identity.ID) error {
	now := time.Now()

	inviteBody := primitive.OrgInvite{
		OrgID:        &orgID,
		InviterID:    &inviterID,
		PendingTeams: teamIDs,
		Email:        email,
		Created:      &now,
		// Null values below
		InviteeID:  nil,
		ApproverID: nil,
		Accepted:   nil,
		Approved:   nil,
	}

	ID, err := identity.NewMutable(&inviteBody)
	if err != nil {
		return err
	}

	invite := envelope.OrgInvite{
		ID:      &ID,
		Version: 1,
		Body:    &inviteBody,
	}

	req, err := i.client.NewRequest("POST", "/org-invites", nil, &invite)
	if err != nil {
		return err
	}

	_, err = i.client.Do(ctx, req, nil)
	return err
}

// Accept executes the accept invite request
func (i *upstreamOrgInvitesClient) Accept(ctx context.Context, org, email, code string) error {
	data := apitypes.InviteAccept{
		Org:   org,
		Email: email,
		Code:  code,
	}

	req, err := i.client.NewRequest("POST", "/org-invites/accept", nil, data)
	if err != nil {
		return err
	}

	_, err = i.client.Do(ctx, req, nil)
	return err
}

// Associate executes the associate invite request
func (i *upstreamOrgInvitesClient) Associate(ctx context.Context, org, email, code string) (*envelope.OrgInvite, error) {
	// Same payload as accept, re-use type
	data := apitypes.InviteAccept{
		Org:   org,
		Email: email,
		Code:  code,
	}

	req, err := i.client.NewRequest("POST", "/org-invites/associate", nil, data)
	if err != nil {
		return nil, err
	}

	invite := envelope.OrgInvite{}
	_, err = i.client.Do(ctx, req, &invite)
	return &invite, err
}

// Approve executes the approve invite request
func (i *OrgInvitesClient) Approve(ctx context.Context, inviteID identity.ID, output ProgressFunc) error {
	req, reqID, err := i.client.NewDaemonRequest("POST", "/org-invites/"+inviteID.String()+"/approve", nil, nil)
	if err != nil {
		return err
	}

	_, err = i.client.DoWithProgress(ctx, req, nil, reqID, output)
	return err
}
