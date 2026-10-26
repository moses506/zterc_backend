package models

// Lead maps to the "Lead" model used by the ZETRC training hub.
type Lead struct {
	Base
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Company   string `json:"company"`
	Source    string `json:"source"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

func (Lead) TableName() string { return "leads" }

// Inquiry maps to the "Inquiry" model.
type Inquiry struct {
	Base
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Subject   string `json:"subject"`
	Message   string `json:"message"`
	Status    string `json:"status"`
}

func (Inquiry) TableName() string { return "inquiries" }

// ProposalRequest maps to the "Proposal_Request" model.
type ProposalRequest struct {
	Base
	OrganizationName   string `json:"organization_name"`
	ContactPerson      string `json:"contact_person"`
	Email              string `json:"email"`
	Phone              string `json:"phone"`
	ProjectDescription string `json:"project_description"`
	Budget             string `json:"budget"`
	Timeline           string `json:"timeline"`
	Status             string `json:"status"`
}

func (ProposalRequest) TableName() string { return "proposal_requests" }
