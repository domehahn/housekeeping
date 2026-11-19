use crate::config::UserPolicyConfig;
use crate::domain::activity::ActivityStatus;
use crate::domain::clock::Clock;
use crate::domain::user::User;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum UserDecision {
    Stale { reason: String },
    Keep { reason: String },
}

pub struct UserPolicy {
    config: UserPolicyConfig,
}

impl UserPolicy {
    pub fn new(config: UserPolicyConfig) -> Self {
        Self { config }
    }

    pub fn evaluate(&self, user: &User, clock: &dyn Clock) -> UserDecision {
        // 1. Protection checks
        if user.is_admin && self.config.protect_admins {
            return UserDecision::Keep {
                reason: "User is an administrator".to_string(),
            };
        }

        if user.is_bot && self.config.protect_bots {
            return UserDecision::Keep {
                reason: "User is a bot / service account".to_string(),
            };
        }

        for username in &self.config.protect_usernames {
            if user.username.eq_ignore_ascii_case(username) {
                return UserDecision::Keep {
                    reason: format!("Matches protected username '{}'", username),
                };
            }
        }

        // 2. Activity evaluation (Fail-closed on Unknown activity)
        let now = clock.now();
        let cutoff = now - self.config.inactive_duration();

        let has_recent_activity = match &user.last_activity_at {
            ActivityStatus::Known(ts) => *ts >= cutoff,
            ActivityStatus::Unknown => {
                return UserDecision::Keep {
                    reason: "Last activity timestamp is unknown (fail-closed safe default)"
                        .to_string(),
                };
            }
            ActivityStatus::Never => false,
        };

        let has_recent_login = match &user.last_login_at {
            ActivityStatus::Known(ts) => *ts >= cutoff,
            ActivityStatus::Unknown => {
                return UserDecision::Keep {
                    reason: "Last login timestamp is unknown (fail-closed safe default)"
                        .to_string(),
                };
            }
            ActivityStatus::Never => false,
        };

        if has_recent_activity || has_recent_login {
            return UserDecision::Keep {
                reason: format!(
                    "User was active within the last {} days",
                    self.config.inactive_days
                ),
            };
        }

        UserDecision::Stale {
            reason: format!(
                "Inactive (no activity or sign-in for over {} days)",
                self.config.inactive_days
            ),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::clock::FixedClock;
    use crate::domain::user::AccessLevel;
    use chrono::{Duration, TimeZone, Utc};

    #[test]
    fn test_stale_user_evaluation() {
        let now = Utc.with_ymd_and_hms(2026, 11, 1, 12, 0, 0).unwrap();
        let clock = FixedClock::new(now);

        let mut config = UserPolicyConfig::default();
        config.inactive_days = 90;
        let policy = UserPolicy::new(config);

        let stale_user = User {
            id: "10".into(),
            username: "jdoe".into(),
            name: "John Doe".into(),
            email: Some("jdoe@example.com".into()),
            state: "active".into(),
            is_bot: false,
            is_admin: false,
            access_level: AccessLevel::Developer,
            created_at: now - Duration::days(365),
            last_activity_at: ActivityStatus::Known(now - Duration::days(120)),
            last_login_at: ActivityStatus::Known(now - Duration::days(130)),
            is_billable: true,
        };

        let decision = policy.evaluate(&stale_user, &clock);
        assert!(matches!(decision, UserDecision::Stale { .. }));

        // Admin protection
        let admin_user = User {
            is_admin: true,
            ..stale_user.clone()
        };
        assert!(matches!(
            policy.evaluate(&admin_user, &clock),
            UserDecision::Keep { .. }
        ));

        // Unknown activity (fail closed)
        let unknown_user = User {
            last_activity_at: ActivityStatus::Unknown,
            ..stale_user.clone()
        };
        assert!(matches!(
            policy.evaluate(&unknown_user, &clock),
            UserDecision::Keep { .. }
        ));
    }
}
