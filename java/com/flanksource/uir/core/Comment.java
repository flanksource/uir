package com.flanksource.uir.core;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
// Temporarily commented out - package mismatch issue
// import com.flanksource.archunit.uir.enums.UIREnums.CommentType;

/**
 * Represents a comment in source code
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class Comment {
    @JsonProperty("text")
    private String text;

    /* Temporarily commented out - package mismatch
    @JsonProperty("type")
    private CommentType type;
    */

    @JsonProperty("location")
    private Location location;

    public Comment() {}

    public Comment(String text, String type, Location location) {
        this.text = text;
        // this.type = type;
        this.location = location;
    }

    public String getText() { return text; }
    public void setText(String text) { this.text = text; }

    /* Temporarily commented out - package mismatch
    public CommentType getType() { return type; }
    public void setType(CommentType type) { this.type = type; }
    */

    public Location getLocation() { return location; }
    public void setLocation(Location location) { this.location = location; }
}
