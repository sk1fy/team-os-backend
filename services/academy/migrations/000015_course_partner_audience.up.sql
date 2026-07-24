-- Partner audience controls for company-owned courses. Absence of a row means
-- the default deny ('none'): partners cannot see or take the course. Owners and
-- admins open a course to all partners or to an explicit list of partner users.
CREATE TABLE course_partner_audiences (
    company_id uuid NOT NULL,
    course_id uuid NOT NULL,
    audience text NOT NULL DEFAULT 'none'
        CHECK (audience IN ('none', 'all_partners', 'selected_partners')),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, course_id),
    FOREIGN KEY (company_id, course_id)
        REFERENCES courses (company_id, id) ON DELETE CASCADE
);

CREATE TABLE course_partner_audience_members (
    company_id uuid NOT NULL,
    course_id uuid NOT NULL,
    partner_user_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, course_id, partner_user_id),
    FOREIGN KEY (company_id, course_id)
        REFERENCES course_partner_audiences (company_id, course_id) ON DELETE CASCADE
);

CREATE INDEX course_partner_audience_members_partner_idx
    ON course_partner_audience_members (company_id, partner_user_id, course_id);
