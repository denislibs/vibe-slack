#[derive(Debug, thiserror::Error)]
pub enum EngineError {
    #[error("mls error: {0}")]
    Mls(String),
    #[error("serialization error: {0}")]
    Serde(String),
    #[error("unknown group: {0}")]
    UnknownGroup(String),
}

impl EngineError {
    pub fn mls(e: impl std::fmt::Display) -> Self {
        EngineError::Mls(e.to_string())
    }
    pub fn serde(e: impl std::fmt::Display) -> Self {
        EngineError::Serde(e.to_string())
    }
}
