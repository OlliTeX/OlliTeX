cp -f *.png ../../public/
cp -f *.svg ../../public/
cp -f *.ico ../../public/

cp -f img/ol-brand/*.png ../../public/img/ol-brand/
cp -f img/ol-brand/*.svg ../../public/img/ol-brand/

# Explicitly include the dark Mallard variant
cp -f img/ol-brand/overleaf-a-ds-solution-mallard-dark.svg ../../public/img/ol-brand/ 

cp -f img/ol-brand/overleaf.svg ../../services/web/frontend/js/shared/svgs
cp -f img/ol-brand/overleaf-*.svg ../../services/web/frontend/js/shared/svgs

# Explicitly copy the dark Mallard variant into shared svgs as well
cp -f img/ol-brand/overleaf-a-ds-solution-mallard-dark.svg ../../services/web/frontend/js/shared/svgs 

