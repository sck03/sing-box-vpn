package manage

import (
	"context"
	"strconv"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/certificates"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

type certificateScheduleSource struct{ service *PlatformService }

func (s *PlatformService) scheduleCertificates(ctx context.Context) error {
	return s.certificateSchedule.Run(ctx, certificateScheduleSource{s}, time.Now())
}

func (a certificateScheduleSource) AppliedCertificates(ctx context.Context, since int64) (int64, []CertificateAsset, error) {
	state, err := a.service.Store.AppliedStateSince(ctx, since)
	if err != nil || state.Config == nil {
		return state.Revision, nil, err
	}
	c, err := decodePlatform(state.Config)
	return state.Revision, c.Certificates, err
}

func (a certificateScheduleSource) RenewalAttempt(ctx context.Context, id string) (time.Time, error) {
	value, err := a.service.Store.Observation(ctx, "certificate-attempt/"+id)
	if err != nil || value == "" {
		return time.Time{}, err
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(seconds, 0), nil
}

func (a certificateScheduleSource) QueueRenewal(ctx context.Context, id string, now time.Time) error {
	if _, err := a.service.Queue(ctx, application.TaskCommand{Operation: "certificate-renew", ID: id}, domain.TaskSubmission{Actor: "scheduler", Source: domain.TaskScheduler}); err != nil {
		return err
	}
	return a.service.Store.Observe(ctx, "certificate-attempt/"+id, strconv.FormatInt(now.Unix(), 10))
}

func (a certificateScheduleSource) RegistrationReady(asset CertificateAsset, material certificates.Material) bool {
	return acmeRegistrationReady(a.service.Root, asset, material)
}
