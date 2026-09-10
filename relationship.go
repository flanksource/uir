package uir

import (
	"github.com/google/uuid"
	"github.com/samber/lo"
)

type RelationshipBuilder struct {
	rel UIRRelationship
}

func (b *RelationshipBuilder) Build() UIRRelationship {
	return b.rel
}

func (b *RelationshipBuilder) Source(path string, start, end int) *RelationshipBuilder {
	b.rel.StartLine = lo.ToPtr(start)
	b.rel.EndLine = lo.ToPtr(end)
	b.rel.Path = path
	return b
}

func (b *RelationshipBuilder) Comments(comments string) *RelationshipBuilder {
	b.rel.Comments = append(b.rel.Comments, NewComment(comments).Build())
	return b
}

func (b *RelationshipBuilder) Text(text string) *RelationshipBuilder {
	b.rel.Content = lo.ToPtr(text)
	return b
}

type UIRRelationship struct {
	Metadata         `json:",inline" gorm:"-"`
	SourceCode       `json:",inline" gorm:"-"`
	ID               uuid.UUID        `json:"id" gorm:"type:uuid;default:uuid_generate_v4();primaryKey"`
	RelationshipType RelationshipType `json:"relationship_type,omitempty"`
	From             *Node            `json:"from,omitempty" gorm:"serializer:json"`
	To               Node             `json:"to,omitempty" gorm:"serializer:json"`
}

func (r UIRRelationship) GetFrom() Node {
	if r.From != nil {
		return *r.From
	}
	return nil
}
func (r UIRRelationship) GetLocation() Location {
	return r.Location
}

func (r UIRRelationship) GetTo() Node {
	return r.To
}

func (r UIRRelationship) GetRelationshipType() RelationshipType {
	return r.RelationshipType
}

func NewRelationship(relType RelationshipType, from, to Node) *RelationshipBuilder {
	return &RelationshipBuilder{
		rel: UIRRelationship{
			RelationshipType: relType,
			From:             &from,
			To:               to,
		},
	}
}

type ASTRelationship = UIRRelationship
