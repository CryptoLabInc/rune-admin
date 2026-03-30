"""
Integration tests for protovalidate with real proto message descriptors.

Verifies that .proto annotation constraints are correctly enforced
at the schema level via protovalidate.
"""
import pytest
import sys
import os

sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../vault'))
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../vault/proto'))

protovalidate = pytest.importorskip("protovalidate")
pb2 = pytest.importorskip("vault_service_pb2")


@pytest.fixture
def validator():
    return protovalidate.Validator()


# ---------------------------------------------------------------------------
# GetPublicKey
# ---------------------------------------------------------------------------

class TestGetPublicKeyProto:
    def test_valid(self, validator):
        validator.validate(pb2.GetPublicKeyRequest(token="abc123"))

    def test_empty_token_rejected(self, validator):
        with pytest.raises(protovalidate.ValidationError):
            validator.validate(pb2.GetPublicKeyRequest(token=""))

    def test_token_at_max_length(self, validator):
        validator.validate(pb2.GetPublicKeyRequest(token="a" * 512))

    def test_token_exceeds_max_length(self, validator):
        with pytest.raises(protovalidate.ValidationError):
            validator.validate(pb2.GetPublicKeyRequest(token="a" * 513))


# ---------------------------------------------------------------------------
# DecryptScores
# ---------------------------------------------------------------------------

class TestDecryptScoresProto:
    def test_valid(self, validator):
        req = pb2.DecryptScoresRequest(
            token="tok", encrypted_blob_b64="AQID", top_k=5
        )
        validator.validate(req)

    def test_top_k_zero_rejected(self, validator):
        """Proto3 int32 default is 0 — must be rejected."""
        req = pb2.DecryptScoresRequest(
            token="tok", encrypted_blob_b64="AQID", top_k=0
        )
        with pytest.raises(protovalidate.ValidationError):
            validator.validate(req)

    def test_top_k_negative_rejected(self, validator):
        req = pb2.DecryptScoresRequest(
            token="tok", encrypted_blob_b64="AQID", top_k=-1
        )
        with pytest.raises(protovalidate.ValidationError):
            validator.validate(req)

    def test_top_k_exceeds_global_max(self, validator):
        req = pb2.DecryptScoresRequest(
            token="tok", encrypted_blob_b64="AQID", top_k=301
        )
        with pytest.raises(protovalidate.ValidationError):
            validator.validate(req)

    def test_top_k_at_boundary_one(self, validator):
        req = pb2.DecryptScoresRequest(
            token="tok", encrypted_blob_b64="a", top_k=1
        )
        validator.validate(req)

    def test_top_k_at_boundary_max(self, validator):
        req = pb2.DecryptScoresRequest(
            token="tok", encrypted_blob_b64="a", top_k=300
        )
        validator.validate(req)

    def test_empty_blob_rejected(self, validator):
        req = pb2.DecryptScoresRequest(
            token="tok", encrypted_blob_b64="", top_k=5
        )
        with pytest.raises(protovalidate.ValidationError):
            validator.validate(req)

    def test_empty_token_rejected(self, validator):
        req = pb2.DecryptScoresRequest(
            token="", encrypted_blob_b64="a", top_k=5
        )
        with pytest.raises(protovalidate.ValidationError):
            validator.validate(req)


# ---------------------------------------------------------------------------
# DecryptMetadata
# ---------------------------------------------------------------------------

class TestDecryptMetadataProto:
    def test_valid(self, validator):
        req = pb2.DecryptMetadataRequest(
            token="tok", encrypted_metadata_list=["blob1", "blob2"]
        )
        validator.validate(req)

    def test_empty_list_rejected(self, validator):
        req = pb2.DecryptMetadataRequest(token="tok")
        with pytest.raises(protovalidate.ValidationError):
            validator.validate(req)

    def test_empty_item_rejected(self, validator):
        req = pb2.DecryptMetadataRequest(
            token="tok", encrypted_metadata_list=["valid", ""]
        )
        with pytest.raises(protovalidate.ValidationError):
            validator.validate(req)

    def test_too_many_items_rejected(self, validator):
        req = pb2.DecryptMetadataRequest(
            token="tok", encrypted_metadata_list=["x"] * 1001
        )
        with pytest.raises(protovalidate.ValidationError):
            validator.validate(req)

    def test_max_items_passes(self, validator):
        req = pb2.DecryptMetadataRequest(
            token="tok", encrypted_metadata_list=["x"] * 1000
        )
        validator.validate(req)
