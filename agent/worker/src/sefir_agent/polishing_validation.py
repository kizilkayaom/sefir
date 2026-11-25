from html.parser import HTMLParser


class HTMLStructureParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.tokens: list[tuple[str, str, tuple[tuple[str, str | None], ...]]] = []
        self.open_tags: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        self.tokens.append(
            ("start", tag, tuple(sorted(attrs, key=lambda item: item[0])))
        )
        if tag != "br":
            self.open_tags.append(tag)

    def handle_endtag(self, tag: str) -> None:
        if not self.open_tags or self.open_tags[-1] != tag:
            raise ValueError("Invalid email HTML structure")
        self.open_tags.pop()
        self.tokens.append(("end", tag, ()))

    def handle_startendtag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag != "br":
            raise ValueError("Invalid email HTML structure")
        self.handle_starttag(tag, attrs)

    def close(self) -> None:
        super().close()
        if self.open_tags:
            raise ValueError("Invalid email HTML structure")
