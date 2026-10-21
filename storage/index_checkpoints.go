package storage

type IndexCheckpoint struct {
	Key         string `gorm:"column:key;primaryKey"`
	Fingerprint string `gorm:"column:fingerprint"`
	Inputs      JSON   `gorm:"column:inputs"`
	Results     JSON   `gorm:"column:results"`
	Heads       JSON   `gorm:"column:heads"`
}

func (IndexCheckpoint) TableName() string { return "index_checkpoints" }
