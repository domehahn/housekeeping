use chrono::{DateTime, Utc};
use std::sync::Arc;

pub trait Clock: Send + Sync {
    fn now(&self) -> DateTime<Utc>;
}

#[derive(Debug, Clone, Default)]
pub struct SystemClock;

impl Clock for SystemClock {
    fn now(&self) -> DateTime<Utc> {
        Utc::now()
    }
}

#[derive(Debug, Clone)]
pub struct FixedClock {
    pub fixed_time: DateTime<Utc>,
}

impl FixedClock {
    pub fn new(time: DateTime<Utc>) -> Self {
        Self { fixed_time: time }
    }
}

impl Clock for FixedClock {
    fn now(&self) -> DateTime<Utc> {
        self.fixed_time
    }
}

pub type DynClock = Arc<dyn Clock>;
