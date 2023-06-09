package grandproductargument

import (
	"bytes"
	"testing"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	"github.com/jsign/curdleproofs/common"
	"github.com/jsign/curdleproofs/msmaccumulator"
	"github.com/jsign/curdleproofs/transcript"
	"github.com/stretchr/testify/require"
)

func TestCompletenessAndSoundess(t *testing.T) {
	t.Parallel()

	n := 128
	rand, err := common.NewRand(0)
	require.NoError(t, err)

	var proof Proof
	{
		transcriptProver := transcript.New([]byte("gprod"))

		crsGs, err := rand.GetG1Affines(n - common.N_BLINDERS)
		require.NoError(t, err)
		crsHs, err := rand.GetG1Affines(common.N_BLINDERS)
		require.NoError(t, err)
		crsH, err := rand.GetG1Jac()
		require.NoError(t, err)
		crs := CRS{
			Gs: crsGs,
			Hs: crsHs,
			H:  crsH,
		}

		bs, err := rand.GetFrs(n - common.N_BLINDERS)
		require.NoError(t, err)
		r_bs, err := rand.GetFrs(common.N_BLINDERS)
		require.NoError(t, err)

		result := fr.One()
		for _, b := range bs {
			result.Mul(&result, &b)
		}

		var B, B_L, B_R bls12381.G1Jac
		_, err = B_L.MultiExp(crsGs, bs, common.MultiExpConf)
		require.NoError(t, err)
		_, err = B_R.MultiExp(crsHs, r_bs, common.MultiExpConf)
		require.NoError(t, err)
		B.AddAssign(&B_L).AddAssign(&B_R)

		proof, err = Prove(
			crs,
			B,
			result,
			bs,
			r_bs,
			transcriptProver,
			rand,
		)
		require.NoError(t, err)
	}

	t.Run("completeness", func(t *testing.T) {
		crs, Gsum, Hsum, B, result, transcriptVerifier, msmAccumulator := genVerifierParameters(t, n)
		ok, err := Verify(
			proof,
			crs,
			Gsum,
			Hsum,
			B,
			result,
			common.N_BLINDERS,
			transcriptVerifier,
			msmAccumulator,
			rand,
		)
		require.NoError(t, err)
		require.True(t, ok)

		ok, err = msmAccumulator.Verify()
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("soundness - wrong result", func(t *testing.T) {
		crs, Gsum, Hsum, B, result, transcriptVerifier, msmAccumulator := genVerifierParameters(t, n)
		one := fr.One()
		var resultPlusOne fr.Element
		resultPlusOne.Add(&result, &one)
		ok, err := Verify(
			proof,
			crs,
			Gsum,
			Hsum,
			B,
			resultPlusOne, // This is the reason why the verifier should not accept the proof.
			common.N_BLINDERS,
			transcriptVerifier,
			msmAccumulator,
			rand,
		)
		require.NoError(t, err)
		require.True(t, ok) // This is OK, because the ultimate check is in the MSM accumulator below.

		ok, err = msmAccumulator.Verify()
		require.NoError(t, err)
		require.False(t, ok) // Note we expect this to be false.
	})

	t.Run("soundness - wrong commitment to Bs", func(t *testing.T) {
		crs, Gsum, Hsum, B, result, transcriptVerifier, msmAccumulator := genVerifierParameters(t, n)
		randScalar, err := rand.GetFr()
		require.NoError(t, err)

		B.ScalarMultiplication(&B, common.FrToBigInt(&randScalar)) // This is the reason why the verifier should not accept the proof.
		ok, err := Verify(
			proof,
			crs,
			Gsum,
			Hsum,
			B,
			result,
			common.N_BLINDERS,
			transcriptVerifier,
			msmAccumulator,
			rand,
		)
		require.NoError(t, err)
		require.True(t, ok) // This is OK, because the ultimate check is in the MSM accumulator below.

		ok, err = msmAccumulator.Verify()
		require.NoError(t, err)
		require.False(t, ok) // Note we expect this to be false.
	})

	t.Run("encode/decode", func(t *testing.T) {
		buf := bytes.NewBuffer(nil)
		require.NoError(t, proof.Serialize(buf))
		expected := buf.Bytes()

		var proof2 Proof
		require.NoError(t, proof2.FromReader(buf))

		buf2 := bytes.NewBuffer(nil)
		require.NoError(t, proof2.Serialize(buf2))

		require.Equal(t, expected, buf2.Bytes())
	})
}

// TOOD(jsign): replicat in other tests.
func genVerifierParameters(t *testing.T, n int) (CRS, bls12381.G1Affine, bls12381.G1Affine, bls12381.G1Jac, fr.Element, *transcript.Transcript, *msmaccumulator.MsmAccumulator) {
	rand, err := common.NewRand(0)
	require.NoError(t, err)

	transcriptVerifier := transcript.New([]byte("gprod"))
	msmAccumulator := msmaccumulator.New()

	crsGs, err := rand.GetG1Affines(n - common.N_BLINDERS)
	require.NoError(t, err)
	crsHs, err := rand.GetG1Affines(common.N_BLINDERS)
	require.NoError(t, err)
	crsH, err := rand.GetG1Jac()
	require.NoError(t, err)
	var Gsum bls12381.G1Affine
	for _, g := range crsGs {
		Gsum.Add(&Gsum, &g)
	}
	var Hsum bls12381.G1Affine
	for _, h := range crsHs {
		Hsum.Add(&Hsum, &h)
	}
	crs := CRS{
		Gs: crsGs,
		Hs: crsHs,
		H:  crsH,
	}
	bs, err := rand.GetFrs(n - common.N_BLINDERS)
	require.NoError(t, err)
	r_bs, err := rand.GetFrs(common.N_BLINDERS)
	require.NoError(t, err)

	result := fr.One()
	for _, b := range bs {
		result.Mul(&result, &b)
	}

	var B, B_L, B_R bls12381.G1Jac
	_, err = B_L.MultiExp(crsGs, bs, common.MultiExpConf)
	require.NoError(t, err)
	_, err = B_R.MultiExp(crsHs, r_bs, common.MultiExpConf)
	require.NoError(t, err)
	B.AddAssign(&B_L).AddAssign(&B_R)

	return crs, Gsum, Hsum, B, result, transcriptVerifier, msmAccumulator
}
