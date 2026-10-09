from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
import unittest
import warnings

import grpc
import joblib
from sklearn.exceptions import InconsistentVersionWarning

from app import InferenceService
from inference_pb2 import InferenceRequest
from inference_pb2_grpc import InferenceStub, add_InferenceServicer_to_server


class InferenceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with warnings.catch_warnings():
            warnings.simplefilter('error', InconsistentVersionWarning)
            model = joblib.load(Path(__file__).parent / 'isolation_forest_model.joblib')
        cls.server = grpc.server(ThreadPoolExecutor(max_workers=2))
        add_InferenceServicer_to_server(InferenceService(model), cls.server)
        port = cls.server.add_insecure_port('127.0.0.1:0')
        cls.server.start()
        cls.channel = grpc.insecure_channel(f'127.0.0.1:{port}')
        cls.stub = InferenceStub(cls.channel)

    @classmethod
    def tearDownClass(cls):
        cls.channel.close()
        cls.server.stop(0).wait()

    def test_saved_model_accepts_41_features(self):
        response = self.stub.Predict(InferenceRequest(features=[0.0] * 41), timeout=5)
        self.assertIn(response.prediction, (-1, 1))

    def test_malformed_feature_vector_returns_an_rpc_error(self):
        with self.assertRaises(grpc.RpcError) as error:
            self.stub.Predict(InferenceRequest(features=[0.0] * 3), timeout=5)
        self.assertEqual(error.exception.code(), grpc.StatusCode.INTERNAL)


if __name__ == '__main__':
    unittest.main()
