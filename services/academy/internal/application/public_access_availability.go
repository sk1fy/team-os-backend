package application

const (
	publicAccessReasonDistributionPaused = "distribution_paused"
	publicAccessReasonCourseBlocked      = "course_blocked"
	publicAccessReasonCourseArchived     = "course_archived"
	publicAccessReasonCourseDeleted      = "course_deleted"
	publicAccessReasonAccessRevoked      = "access_revoked"
	publicAccessReasonAccessExpired      = "access_expired"
	publicAccessReasonCampaignPaused     = "campaign_paused"
	publicAccessReasonCampaignRevoked    = "campaign_revoked"
	publicAccessReasonCampaignClosed     = "campaign_closed"
	publicAccessReasonAlreadyActivated   = "already_activated"
	publicAccessReasonVersionUnavailable = "version_unavailable"
	publicAccessReasonUnavailable        = "unavailable"
)

type publicAccessAvailability struct {
	Available bool
	Reason    string
	Message   string
}

func availablePublicAccess() publicAccessAvailability {
	return publicAccessAvailability{Available: true}
}

func unavailablePublicAccess(reason, message string) publicAccessAvailability {
	return publicAccessAvailability{Reason: reason, Message: message}
}

func personalAccessAvailability(
	status, lifecycle, distribution, versionStatus string,
	deadlineExpired bool,
) publicAccessAvailability {
	switch {
	case lifecycle == "deleted":
		return unavailablePublicAccess(publicAccessReasonCourseDeleted, "Курс удалён")
	case distribution == "blocked":
		return unavailablePublicAccess(publicAccessReasonCourseBlocked, "Курс заблокирован администрацией")
	case lifecycle == "archived":
		return unavailablePublicAccess(publicAccessReasonCourseArchived, "Курс находится в архиве")
	case status == "revoked" || status == "closed":
		return unavailablePublicAccess(publicAccessReasonAccessRevoked, "Доступ отозван автором курса")
	case deadlineExpired:
		return unavailablePublicAccess(publicAccessReasonAccessExpired, "Срок доступа истёк")
	case status == "activated":
		return unavailablePublicAccess(publicAccessReasonAlreadyActivated, "Доступ уже активирован")
	case distribution == "paused":
		return unavailablePublicAccess(publicAccessReasonDistributionPaused, "Распространение курса временно приостановлено")
	case versionStatus != "published":
		return unavailablePublicAccess(publicAccessReasonVersionUnavailable, "Версия курса недоступна")
	case status == "issued" && lifecycle == "active" && distribution == "active":
		return availablePublicAccess()
	default:
		return unavailablePublicAccess(publicAccessReasonUnavailable, "Курс сейчас недоступен")
	}
}

func campaignAccessAvailability(
	status, lifecycle, distribution, versionStatus string,
	hasEnrollment, enrollmentExpired bool,
) publicAccessAvailability {
	switch {
	case lifecycle == "deleted":
		return unavailablePublicAccess(publicAccessReasonCourseDeleted, "Курс удалён")
	case distribution == "blocked":
		return unavailablePublicAccess(publicAccessReasonCourseBlocked, "Курс заблокирован администрацией")
	case lifecycle == "archived":
		return unavailablePublicAccess(publicAccessReasonCourseArchived, "Курс находится в архиве")
	case enrollmentExpired:
		return unavailablePublicAccess(publicAccessReasonAccessExpired, "Срок доступа истёк")
	case hasEnrollment:
		return unavailablePublicAccess(publicAccessReasonAlreadyActivated, "Доступ уже активирован")
	case distribution == "paused":
		return unavailablePublicAccess(publicAccessReasonDistributionPaused, "Распространение курса временно приостановлено")
	case status == "paused":
		return unavailablePublicAccess(publicAccessReasonCampaignPaused, "Кампания временно приостановлена")
	case status == "revoked":
		return unavailablePublicAccess(publicAccessReasonCampaignRevoked, "Кампания отозвана")
	case status == "closed":
		return unavailablePublicAccess(publicAccessReasonCampaignClosed, "Кампания закрыта")
	case versionStatus != "published":
		return unavailablePublicAccess(publicAccessReasonVersionUnavailable, "Версия курса недоступна")
	case status == "active" && lifecycle == "active" && distribution == "active":
		return availablePublicAccess()
	default:
		return unavailablePublicAccess(publicAccessReasonUnavailable, "Курс сейчас недоступен")
	}
}

func availabilityPointers(value publicAccessAvailability) (*string, *string) {
	if value.Available {
		return nil, nil
	}
	reason, message := value.Reason, value.Message
	return &reason, &message
}
