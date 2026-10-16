"""Файловое хранилище: пути, лимиты, защита от traversal (ТЗ §13, ADD §11)."""

import io

import pytest

from linguastack.storage.files import (
    DisallowedExtension,
    FileStore,
    FileTooLarge,
    UnsafePath,
)

USER = "11111111-1111-1111-1111-111111111111"
BOOK = "22222222-2222-2222-2222-222222222222"


def test_path_never_contains_user_supplied_name(tmp_path):
    store = FileStore(tmp_path, max_bytes=1024)
    path = store.save_original(USER, BOOK, ".txt", b"hello")
    # ТЗ §2.2: имена на диске непрогнозируемы и не зависят от имени,
    # которое задал пользователь.
    assert path.name == "original.txt"
    assert USER in str(path) and BOOK in str(path)
    assert path.read_bytes() == b"hello"


@pytest.mark.parametrize(
    "member",
    [
        "../escape.md",
        "/absolute/path.md",
        "nested/../../escape.md",
        "a/b/../../../escape.md",
        "../../etc/passwd",
        "./../escape.md",
    ],
)
def test_zip_member_escaping_target_dir_is_rejected(tmp_path, member):
    store = FileStore(tmp_path, max_bytes=1024)
    with pytest.raises(UnsafePath):
        store.safe_extract_member(tmp_path / "out", member)


@pytest.mark.parametrize("member", ["ch1.md", "nested/ch2.md", "a/b/c/ch3.md"])
def test_legitimate_zip_members_accepted(tmp_path, member):
    store = FileStore(tmp_path, max_bytes=1024)
    target = tmp_path / "out"
    resolved = store.safe_extract_member(target, member)
    assert resolved.is_relative_to(target.resolve())


def test_backslash_member_is_rejected(tmp_path):
    """Windows-разделитель не должен обходить проверку на POSIX."""
    store = FileStore(tmp_path, max_bytes=1024)
    with pytest.raises(UnsafePath):
        store.safe_extract_member(tmp_path / "out", "..\\..\\escape.md")


def test_oversized_bytes_rejected(tmp_path):
    store = FileStore(tmp_path, max_bytes=10)
    with pytest.raises(FileTooLarge):
        store.save_original(USER, BOOK, ".txt", b"x" * 11)


def test_oversized_stream_rejected_without_writing_whole_file(tmp_path):
    """Лимит должен срабатывать по ходу чтения, а не после исчерпания диска."""
    store = FileStore(tmp_path, max_bytes=10)
    with pytest.raises(FileTooLarge):
        store.save_original(USER, BOOK, ".txt", io.BytesIO(b"y" * 5000))
    # Частично записанный файл не должен остаться.
    assert not (store.book_dir(USER, BOOK) / "original.txt").exists()


def test_stream_within_limit_is_saved(tmp_path):
    store = FileStore(tmp_path, max_bytes=1024)
    path = store.save_original(USER, BOOK, ".md", io.BytesIO(b"# Title"))
    assert path.read_bytes() == b"# Title"


def test_unknown_extension_rejected(tmp_path):
    store = FileStore(tmp_path, max_bytes=1024)
    with pytest.raises(DisallowedExtension):
        store.save_original(USER, BOOK, ".exe", b"MZ")


def test_extension_check_is_case_insensitive(tmp_path):
    store = FileStore(tmp_path, max_bytes=1024)
    assert store.save_original(USER, BOOK, ".EPUB", b"data").name == "original.epub"


def test_normalized_document_saved_as_json(tmp_path):
    import json

    store = FileStore(tmp_path, max_bytes=1024 * 1024)
    path = store.save_normalized(USER, BOOK, {"meta": {"title": "T"}, "chapters": []})
    assert json.loads(path.read_text(encoding="utf-8"))["meta"]["title"] == "T"


def test_open_original_round_trip(tmp_path):
    store = FileStore(tmp_path, max_bytes=1024)
    store.save_original(USER, BOOK, ".txt", b"content")
    with store.open_original(USER, BOOK, ".txt") as handle:
        assert handle.read() == b"content"


def test_delete_book_removes_directory(tmp_path):
    store = FileStore(tmp_path, max_bytes=1024)
    store.save_original(USER, BOOK, ".txt", b"data")
    store.delete_book(USER, BOOK)
    assert not store.book_dir(USER, BOOK).exists()


def test_delete_book_is_idempotent(tmp_path):
    """Удаление книги без файлов не должно падать — иначе чистка срывается."""
    store = FileStore(tmp_path, max_bytes=1024)
    store.delete_book(USER, BOOK)
    store.delete_book(USER, BOOK)


def test_delete_book_does_not_touch_other_books(tmp_path):
    store = FileStore(tmp_path, max_bytes=1024)
    other_book = "33333333-3333-3333-3333-333333333333"
    store.save_original(USER, BOOK, ".txt", b"a")
    store.save_original(USER, other_book, ".txt", b"b")
    store.delete_book(USER, BOOK)
    assert store.book_dir(USER, other_book).exists()


def test_identifiers_are_validated_as_uuids(tmp_path):
    """Идентификатор из запроса не должен становиться частью пути как есть."""
    store = FileStore(tmp_path, max_bytes=1024)
    with pytest.raises(UnsafePath):
        store.book_dir("../../etc", BOOK)
    with pytest.raises(UnsafePath):
        store.book_dir(USER, "../../etc")
