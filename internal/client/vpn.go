package client

import (
	"context"
	"encoding/base64"
	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/dto/input/query"
)

type VPNPeerAddress struct {
	AssignedIPAddress string `json:"assignedIpAddress"`
}
type VPNPeer struct {
	PublicKey    string         `json:"publicKey"`
	IPv4         VPNPeerAddress `json:"ipv4"`
	IPv6         VPNPeerAddress `json:"ipv6"`
	Setup        string         `json:"setup"`
	SetupLinux   string         `json:"setupLinux"`
	SetupMacOS   string         `json:"setupMacOS"`
	SetupWindows string         `json:"setupWindows"`
}
type vpnEnvelope struct {
	Peer VPNPeer `json:"peer"`
}

// VPNEncodedKey encodes the textual standard-base64 key, retaining URL-base64 padding.
func VPNEncodedKey(key string) string { return base64.URLEncoding.EncodeToString([]byte(key)) }
func (c *Client) CreateVPNPeer(ctx context.Context, id, key string) (VPNPeer, error) {
	ctx, s := c.operation(ctx, "CreateVPNPeer")
	p, e := input[path.ProjectId](map[string]any{"Id": id})
	if e != nil {
		return VPNPeer{}, e
	}
	b, e := input[body.PostProjectVpn](map[string]any{"publicKey": key})
	if e != nil {
		return VPNPeer{}, e
	}
	r, e := c.sdk.PostProjectVpn(ctx, p, b)
	v, e := finish[vpnEnvelope](s, r, e)
	return v.Peer, e
}
func (c *Client) GetVPNPeer(ctx context.Context, id, key string) (VPNPeer, error) {
	ctx, s := c.operation(ctx, "GetVPNPeer")
	p, e := input[path.ProjectIdBase64PublicKey](map[string]any{"Id": id, "Base64PublicKey": VPNEncodedKey(key)})
	if e != nil {
		return VPNPeer{}, e
	}
	r, e := c.sdk.GetProjectVpn(ctx, p, query.GetProjectVpn{})
	v, e := finish[vpnEnvelope](s, r, e)
	return v.Peer, e
}
func (c *Client) DeleteVPNPeer(ctx context.Context, id, key string) error {
	ctx, s := c.operation(ctx, "DeleteVPNPeer")
	p, e := input[path.ProjectId](map[string]any{"Id": id})
	if e != nil {
		return e
	}
	b, e := input[body.PostProjectVpn](map[string]any{"publicKey": key})
	if e != nil {
		return e
	}
	r, e := c.sdk.DeleteProjectVpn(ctx, p, b)
	_, e = finish[struct {
		Success bool `json:"success"`
	}](s, r, e)
	return e
}
